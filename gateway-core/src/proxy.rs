use std::collections::HashMap;
use std::time::Duration;

use anyhow::Result;
use bytes::Bytes;
use hyper::{HeaderMap, Method, StatusCode};
use reqwest::Client;
use serde::{Deserialize, Serialize};
use tracing::{debug, error, info, warn};

use crate::config::ProxyConfig;
use crate::error::GatewayError;
use crate::routing::{BackendProtocol, Route};

/// Response from proxying a request to a backend.
#[derive(Debug)]
pub struct ProxyResponse {
    pub status: StatusCode,
    pub headers: HeaderMap,
    pub body: Bytes,
    pub latency_ms: u64,
}

/// The reverse proxy engine. Handles forwarding requests to backends.
pub struct ProxyEngine {
    client: Client,
    config: ProxyConfig,
}

impl ProxyEngine {
    pub fn new(config: &ProxyConfig) -> Result<Self> {
        let client = Client::builder()
            .connect_timeout(Duration::from_millis(config.connect_timeout_ms))
            .timeout(Duration::from_millis(config.request_timeout_ms))
            .pool_max_idle_per_host(100)
            .pool_idle_timeout(Duration::from_secs(90))
            .redirect(reqwest::redirect::Policy::none())
            .build()?;

        Ok(Self {
            client,
            config: config.clone(),
        })
    }

    /// Forward a request to the backend with retries.
    pub async fn forward(
        &self,
        route: &Route,
        method: Method,
        path: &str,
        headers: HeaderMap,
        body: Bytes,
        path_params: &HashMap<String, String>,
    ) -> Result<ProxyResponse, GatewayError> {
        let target_url = self.build_target_url(route, path, path_params);
        let max_retries = route.target.retries.unwrap_or(self.config.max_retries);

        let mut last_error = None;

        for attempt in 0..=max_retries {
            if attempt > 0 {
                let backoff = Duration::from_millis(
                    self.config.retry_backoff_ms * 2u64.pow(attempt - 1),
                );
                tokio::time::sleep(backoff).await;
                debug!(
                    attempt = attempt,
                    service = &route.target.service_name,
                    "Retrying request"
                );
            }

            match self
                .send_request(&target_url, method.clone(), &headers, body.clone(), route)
                .await
            {
                Ok(response) => {
                    // Don't retry on client errors (4xx)
                    if response.status.is_server_error() && attempt < max_retries {
                        last_error = Some(GatewayError::BackendUnavailable {
                            service: route.target.service_name.clone(),
                        });
                        continue;
                    }
                    return Ok(response);
                }
                Err(e) => {
                    warn!(
                        attempt = attempt,
                        error = %e,
                        service = &route.target.service_name,
                        "Request failed"
                    );
                    last_error = Some(e);
                }
            }
        }

        Err(last_error.unwrap_or(GatewayError::BackendUnavailable {
            service: route.target.service_name.clone(),
        }))
    }

    async fn send_request(
        &self,
        url: &str,
        method: Method,
        headers: &HeaderMap,
        body: Bytes,
        route: &Route,
    ) -> Result<ProxyResponse, GatewayError> {
        let start = std::time::Instant::now();

        let mut request = self.client.request(method, url);

        // Forward headers, filtering hop-by-hop headers
        for (name, value) in headers.iter() {
            if !is_hop_by_hop_header(name.as_str()) {
                request = request.header(name.clone(), value.clone());
            }
        }

        // Add custom headers from route config
        for (name, value) in &route.target.headers {
            request = request.header(name.as_str(), value.as_str());
        }

        // Add gateway identifier headers
        request = request.header("X-NexusGate-Route-Id", route.id.to_string());
        request = request.header("X-Forwarded-By", "NexusGate/0.1");

        // Set body
        request = request.body(body);

        // Apply per-route timeout if specified
        if let Some(timeout_ms) = route.target.timeout_ms {
            request = request.timeout(Duration::from_millis(timeout_ms));
        }

        let response = request.send().await.map_err(|e| {
            if e.is_timeout() {
                GatewayError::BackendTimeout {
                    service: route.target.service_name.clone(),
                    timeout_ms: route
                        .target
                        .timeout_ms
                        .unwrap_or(self.config.request_timeout_ms),
                }
            } else if e.is_connect() {
                GatewayError::BackendUnavailable {
                    service: route.target.service_name.clone(),
                }
            } else {
                GatewayError::Proxy(e.to_string())
            }
        })?;

        let latency_ms = start.elapsed().as_millis() as u64;
        let status = response.status();
        let resp_headers = response.headers().clone();
        let resp_body = response
            .bytes()
            .await
            .map_err(|e| GatewayError::Proxy(e.to_string()))?;

        // Emit metrics
        metrics::counter!("gateway_proxy_requests_total", "service" => route.target.service_name.clone(), "status" => status.as_u16().to_string()).increment(1);
        metrics::histogram!("gateway_proxy_latency_ms", "service" => route.target.service_name.clone()).record(latency_ms as f64);

        Ok(ProxyResponse {
            status,
            headers: resp_headers,
            body: resp_body,
            latency_ms,
        })
    }

    fn build_target_url(
        &self,
        route: &Route,
        original_path: &str,
        params: &HashMap<String, String>,
    ) -> String {
        let base = route.target.upstream_url.trim_end_matches('/');

        // Apply path rewrite if configured
        let path = if let Some(ref rewrite) = route.target.rewrite_path {
            rewrite.clone()
        } else if let Some(ref prefix) = route.target.strip_prefix {
            original_path
                .strip_prefix(prefix.as_str())
                .unwrap_or(original_path)
                .to_string()
        } else {
            original_path.to_string()
        };

        // Replace path parameters
        let mut final_path = path;
        for (key, value) in params {
            final_path = final_path.replace(&format!("{{{}}}", key), value);
        }

        format!("{}{}", base, final_path)
    }
}

fn is_hop_by_hop_header(name: &str) -> bool {
    matches!(
        name.to_lowercase().as_str(),
        "connection"
            | "keep-alive"
            | "proxy-authenticate"
            | "proxy-authorization"
            | "te"
            | "trailers"
            | "transfer-encoding"
            | "upgrade"
    )
}
