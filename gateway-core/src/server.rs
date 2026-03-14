use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Instant;

use anyhow::Result;
use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use hyper::body::Incoming;
use hyper::server::conn::http1;
use hyper::service::service_fn;
use hyper::{Request, Response, StatusCode};
use hyper_util::rt::TokioIo;
use tokio::net::TcpListener;
use tracing::{debug, error, info, warn};

use crate::config::GatewayConfig;
use crate::error::GatewayError;
use crate::middleware::{CircuitBreaker, CircuitState, MiddlewarePipeline, RateLimiter};
use crate::proxy::ProxyEngine;
use crate::routing::RouteTable;

pub struct GatewayServer {
    config: GatewayConfig,
    route_table: Arc<RouteTable>,
}

struct RequestContext {
    route_table: Arc<RouteTable>,
    proxy_engine: Arc<ProxyEngine>,
    pipeline: Arc<MiddlewarePipeline>,
}

impl GatewayServer {
    pub fn new(config: GatewayConfig, route_table: Arc<RouteTable>) -> Self {
        Self {
            config,
            route_table,
        }
    }

    pub async fn run(self) -> Result<()> {
        let addr = self.config.server.listen_addr;
        let listener = TcpListener::bind(addr).await?;
        info!(addr = %addr, "Gateway listening");

        let proxy_engine = Arc::new(ProxyEngine::new(&self.config.proxy)?);

        let rate_limiter = Arc::new(RateLimiter::new(1000, 2000));
        let circuit_breaker = Arc::new(CircuitBreaker::new(
            5,
            3,
            std::time::Duration::from_secs(30),
        ));
        let pipeline = Arc::new(MiddlewarePipeline::new(rate_limiter, circuit_breaker));

        loop {
            let (stream, remote_addr) = listener.accept().await?;
            let io = TokioIo::new(stream);

            let ctx = Arc::new(RequestContext {
                route_table: self.route_table.clone(),
                proxy_engine: proxy_engine.clone(),
                pipeline: pipeline.clone(),
            });

            tokio::spawn(async move {
                let service = service_fn(move |req| {
                    let ctx = ctx.clone();
                    async move { handle_request(req, ctx, remote_addr).await }
                });

                if let Err(err) = http1::Builder::new()
                    .serve_connection(io, service)
                    .await
                {
                    if !err.is_incomplete_message() {
                        debug!(error = %err, "Connection error");
                    }
                }
            });
        }
    }
}

async fn handle_request(
    req: Request<Incoming>,
    ctx: Arc<RequestContext>,
    remote_addr: SocketAddr,
) -> Result<Response<Full<Bytes>>, hyper::Error> {
    let start = Instant::now();
    let method = req.method().clone();
    let uri = req.uri().clone();
    let path = uri.path().to_string();

    debug!(method = %method, path = %path, remote = %remote_addr, "Incoming request");

    // Health check endpoint
    if path == "/health" || path == "/healthz" {
        return Ok(json_response(
            StatusCode::OK,
            r#"{"status":"healthy","version":"0.1.0"}"#,
        ));
    }

    // Readiness probe
    if path == "/ready" || path == "/readyz" {
        return Ok(json_response(
            StatusCode::OK,
            r#"{"status":"ready"}"#,
        ));
    }

    // Extract consumer ID from headers (for rate limiting)
    let consumer_id = req
        .headers()
        .get("X-NexusGate-Consumer")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("anonymous")
        .to_string();

    // ── Rate Limiting ──
    if let Err(retry_after) = ctx.pipeline.rate_limiter.check(&consumer_id) {
        let err = GatewayError::RateLimitExceeded {
            consumer_id: consumer_id.clone(),
        };
        metrics::counter!("gateway_rate_limited_total").increment(1);
        let mut resp = json_response(err.status_code(), &err.to_json_body());
        resp.headers_mut().insert(
            "Retry-After",
            hyper::header::HeaderValue::from_str(&format!("{}", retry_after.as_secs() + 1))
                .unwrap(),
        );
        return Ok(resp);
    }

    // ── Route Matching ──
    let (route, params) = match ctx.route_table.match_route(method.as_str(), &path) {
        Some(r) => r,
        None => {
            let err = GatewayError::RouteNotFound {
                method: method.to_string(),
                path: path.clone(),
            };
            metrics::counter!("gateway_route_not_found_total").increment(1);
            return Ok(json_response(err.status_code(), &err.to_json_body()));
        }
    };

    // ── Circuit Breaker ──
    if let Err(state) = ctx
        .pipeline
        .circuit_breaker
        .can_request(&route.target.service_name)
    {
        let err = GatewayError::CircuitBreakerOpen {
            service: route.target.service_name.clone(),
        };
        return Ok(json_response(err.status_code(), &err.to_json_body()));
    }

    // ── Extract request body ──
    let headers = req.headers().clone();
    let body = req
        .collect()
        .await
        .map(|b| b.to_bytes())
        .unwrap_or_default();

    // ── Proxy to backend ──
    match ctx
        .proxy_engine
        .forward(&route, method.clone(), &path, headers, body, &params)
        .await
    {
        Ok(proxy_resp) => {
            ctx.pipeline
                .circuit_breaker
                .record_success(&route.target.service_name);

            let latency = start.elapsed().as_millis() as u64;
            info!(
                method = %method,
                path = %path,
                status = proxy_resp.status.as_u16(),
                latency_ms = latency,
                service = &route.target.service_name,
                "Request completed"
            );

            let mut response = Response::builder()
                .status(proxy_resp.status)
                .body(Full::new(proxy_resp.body))
                .unwrap();

            // Forward response headers
            for (name, value) in proxy_resp.headers.iter() {
                if !is_hop_by_hop_header(name.as_str()) {
                    response.headers_mut().insert(name.clone(), value.clone());
                }
            }

            // Add gateway headers
            response.headers_mut().insert(
                "X-NexusGate-Latency-Ms",
                hyper::header::HeaderValue::from_str(&latency.to_string()).unwrap(),
            );
            response.headers_mut().insert(
                "X-NexusGate-Route-Id",
                hyper::header::HeaderValue::from_str(&route.id.to_string()).unwrap(),
            );

            Ok(response)
        }
        Err(e) => {
            ctx.pipeline
                .circuit_breaker
                .record_failure(&route.target.service_name);

            warn!(
                method = %method,
                path = %path,
                error = %e,
                service = &route.target.service_name,
                "Proxy request failed"
            );

            Ok(json_response(e.status_code(), &e.to_json_body()))
        }
    }
}

fn json_response(status: StatusCode, body: &str) -> Response<Full<Bytes>> {
    Response::builder()
        .status(status)
        .header("Content-Type", "application/json")
        .header("X-Powered-By", "NexusGate/0.1")
        .body(Full::new(Bytes::from(body.to_string())))
        .unwrap()
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
