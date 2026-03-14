use std::net::SocketAddr;
use std::sync::Arc;

use anyhow::Result;
use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use hyper::body::Incoming;
use hyper::server::conn::http1;
use hyper::service::service_fn;
use hyper::{Method, Request, Response, StatusCode};
use hyper_util::rt::TokioIo;
use serde::{Deserialize, Serialize};
use tokio::net::TcpListener;
use tracing::{error, info};
use uuid::Uuid;

use crate::routing::{BackendProtocol, Route, RouteMetadata, RouteTable, RouteTarget};

/// REST API for managing gateway routes at runtime.
/// Used by the AI Orchestrator and Contract Engine to register/update routes.
pub async fn start_management_api(addr: SocketAddr, route_table: Arc<RouteTable>) -> Result<()> {
    let listener = TcpListener::bind(addr).await?;
    info!(addr = %addr, "Management API listening");

    loop {
        let (stream, _) = listener.accept().await?;
        let io = TokioIo::new(stream);
        let routes = route_table.clone();

        tokio::spawn(async move {
            let service = service_fn(move |req| {
                let routes = routes.clone();
                async move { handle_management_request(req, routes).await }
            });

            if let Err(err) = http1::Builder::new().serve_connection(io, service).await {
                if !err.is_incomplete_message() {
                    error!(error = %err, "Management API connection error");
                }
            }
        });
    }
}

async fn handle_management_request(
    req: Request<Incoming>,
    route_table: Arc<RouteTable>,
) -> Result<Response<Full<Bytes>>, hyper::Error> {
    let method = req.method().clone();
    let path = req.uri().path().to_string();

    match (method, path.as_str()) {
        // List all routes
        (Method::GET, "/api/v1/routes") => {
            let routes = route_table.list_routes();
            let body = serde_json::to_string(&ListRoutesResponse {
                routes: routes.iter().map(RouteInfo::from).collect(),
                total: routes.len(),
            })
            .unwrap();
            Ok(json_response(StatusCode::OK, &body))
        }

        // Get a specific route
        (Method::GET, path) if path.starts_with("/api/v1/routes/") => {
            let id_str = path.trim_start_matches("/api/v1/routes/");
            match Uuid::parse_str(id_str) {
                Ok(id) => match route_table.get_route(&id) {
                    Some(route) => {
                        let body = serde_json::to_string(&RouteInfo::from(&route)).unwrap();
                        Ok(json_response(StatusCode::OK, &body))
                    }
                    None => Ok(json_response(
                        StatusCode::NOT_FOUND,
                        r#"{"error":"Route not found"}"#,
                    )),
                },
                Err(_) => Ok(json_response(
                    StatusCode::BAD_REQUEST,
                    r#"{"error":"Invalid route ID"}"#,
                )),
            }
        }

        // Register a new route
        (Method::POST, "/api/v1/routes") => {
            let body = req.collect().await.map(|b| b.to_bytes()).unwrap_or_default();
            match serde_json::from_slice::<CreateRouteRequest>(&body) {
                Ok(create_req) => {
                    let route = Route {
                        id: Uuid::new_v4(),
                        path_pattern: create_req.path_pattern,
                        methods: create_req.methods,
                        target: RouteTarget {
                            service_name: create_req.service_name.clone(),
                            upstream_url: create_req.upstream_url,
                            protocol: create_req.protocol.unwrap_or(BackendProtocol::Http),
                            timeout_ms: create_req.timeout_ms,
                            retries: create_req.retries,
                            strip_prefix: create_req.strip_prefix,
                            rewrite_path: create_req.rewrite_path,
                            headers: create_req.headers.unwrap_or_default(),
                        },
                        contract_id: create_req.contract_id,
                        metadata: RouteMetadata {
                            service_name: create_req.service_name,
                            version: create_req.version.unwrap_or_else(|| "1.0".to_string()),
                            tags: create_req.tags.unwrap_or_default(),
                            description: create_req.description,
                        },
                        middleware: create_req.middleware.unwrap_or_default(),
                        enabled: true,
                        created_at: chrono::Utc::now(),
                        updated_at: chrono::Utc::now(),
                    };

                    match route_table.add_route(route.clone()) {
                        Ok(id) => {
                            info!(route_id = %id, path = &route.path_pattern, "Route registered");
                            let body = serde_json::to_string(&RouteInfo::from(&route)).unwrap();
                            Ok(json_response(StatusCode::CREATED, &body))
                        }
                        Err(e) => {
                            let body = serde_json::json!({"error": e}).to_string();
                            Ok(json_response(StatusCode::CONFLICT, &body))
                        }
                    }
                }
                Err(e) => {
                    let body = serde_json::json!({"error": format!("Invalid request: {}", e)}).to_string();
                    Ok(json_response(StatusCode::BAD_REQUEST, &body))
                }
            }
        }

        // Delete a route
        (Method::DELETE, path) if path.starts_with("/api/v1/routes/") => {
            let id_str = path.trim_start_matches("/api/v1/routes/");
            match Uuid::parse_str(id_str) {
                Ok(id) => match route_table.remove_route(&id) {
                    Some(_) => {
                        info!(route_id = %id, "Route removed");
                        Ok(json_response(StatusCode::NO_CONTENT, ""))
                    }
                    None => Ok(json_response(
                        StatusCode::NOT_FOUND,
                        r#"{"error":"Route not found"}"#,
                    )),
                },
                Err(_) => Ok(json_response(
                    StatusCode::BAD_REQUEST,
                    r#"{"error":"Invalid route ID"}"#,
                )),
            }
        }

        // Health check
        (Method::GET, "/health") => Ok(json_response(
            StatusCode::OK,
            r#"{"status":"healthy","component":"management-api"}"#,
        )),

        _ => Ok(json_response(
            StatusCode::NOT_FOUND,
            r#"{"error":"Endpoint not found"}"#,
        )),
    }
}

// ─── Request/Response Types ──────────────────────────────────

#[derive(Debug, Deserialize)]
struct CreateRouteRequest {
    path_pattern: String,
    methods: Vec<String>,
    service_name: String,
    upstream_url: String,
    protocol: Option<BackendProtocol>,
    timeout_ms: Option<u64>,
    retries: Option<u32>,
    strip_prefix: Option<String>,
    rewrite_path: Option<String>,
    headers: Option<std::collections::HashMap<String, String>>,
    contract_id: Option<Uuid>,
    version: Option<String>,
    tags: Option<Vec<String>>,
    description: Option<String>,
    middleware: Option<Vec<String>>,
}

#[derive(Debug, Serialize)]
struct ListRoutesResponse {
    routes: Vec<RouteInfo>,
    total: usize,
}

#[derive(Debug, Serialize)]
struct RouteInfo {
    id: Uuid,
    path_pattern: String,
    methods: Vec<String>,
    service_name: String,
    upstream_url: String,
    enabled: bool,
    contract_id: Option<Uuid>,
    request_count: u64,
}

impl RouteInfo {
    fn from(route: &Route) -> Self {
        Self {
            id: route.id,
            path_pattern: route.path_pattern.clone(),
            methods: route.methods.clone(),
            service_name: route.target.service_name.clone(),
            upstream_url: route.target.upstream_url.clone(),
            enabled: route.enabled,
            contract_id: route.contract_id,
            request_count: 0,
        }
    }
}

fn json_response(status: StatusCode, body: &str) -> Response<Full<Bytes>> {
    Response::builder()
        .status(status)
        .header("Content-Type", "application/json")
        .body(Full::new(Bytes::from(body.to_string())))
        .unwrap()
}
