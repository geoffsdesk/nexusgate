use hyper::StatusCode;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum GatewayError {
    #[error("Route not found: {method} {path}")]
    RouteNotFound { method: String, path: String },

    #[error("Backend unavailable: {service}")]
    BackendUnavailable { service: String },

    #[error("Backend timeout: {service} after {timeout_ms}ms")]
    BackendTimeout { service: String, timeout_ms: u64 },

    #[error("Circuit breaker open for service: {service}")]
    CircuitBreakerOpen { service: String },

    #[error("Rate limit exceeded for consumer: {consumer_id}")]
    RateLimitExceeded { consumer_id: String },

    #[error("Authentication required")]
    Unauthorized,

    #[error("Insufficient permissions: {detail}")]
    Forbidden { detail: String },

    #[error("Contract violation: {detail}")]
    ContractViolation { detail: String },

    #[error("Invalid request: {detail}")]
    BadRequest { detail: String },

    #[error("Internal gateway error: {0}")]
    Internal(#[from] anyhow::Error),

    #[error("Proxy error: {0}")]
    Proxy(String),

    #[error("Configuration error: {0}")]
    Config(String),
}

impl GatewayError {
    pub fn status_code(&self) -> StatusCode {
        match self {
            Self::RouteNotFound { .. } => StatusCode::NOT_FOUND,
            Self::BackendUnavailable { .. } => StatusCode::BAD_GATEWAY,
            Self::BackendTimeout { .. } => StatusCode::GATEWAY_TIMEOUT,
            Self::CircuitBreakerOpen { .. } => StatusCode::SERVICE_UNAVAILABLE,
            Self::RateLimitExceeded { .. } => StatusCode::TOO_MANY_REQUESTS,
            Self::Unauthorized => StatusCode::UNAUTHORIZED,
            Self::Forbidden { .. } => StatusCode::FORBIDDEN,
            Self::ContractViolation { .. } => StatusCode::UNPROCESSABLE_ENTITY,
            Self::BadRequest { .. } => StatusCode::BAD_REQUEST,
            Self::Internal(_) => StatusCode::INTERNAL_SERVER_ERROR,
            Self::Proxy(_) => StatusCode::BAD_GATEWAY,
            Self::Config(_) => StatusCode::INTERNAL_SERVER_ERROR,
        }
    }

    pub fn to_json_body(&self) -> String {
        serde_json::json!({
            "error": {
                "code": self.status_code().as_u16(),
                "type": self.error_type(),
                "message": self.to_string(),
            }
        })
        .to_string()
    }

    fn error_type(&self) -> &'static str {
        match self {
            Self::RouteNotFound { .. } => "ROUTE_NOT_FOUND",
            Self::BackendUnavailable { .. } => "BACKEND_UNAVAILABLE",
            Self::BackendTimeout { .. } => "BACKEND_TIMEOUT",
            Self::CircuitBreakerOpen { .. } => "CIRCUIT_BREAKER_OPEN",
            Self::RateLimitExceeded { .. } => "RATE_LIMIT_EXCEEDED",
            Self::Unauthorized => "UNAUTHORIZED",
            Self::Forbidden { .. } => "FORBIDDEN",
            Self::ContractViolation { .. } => "CONTRACT_VIOLATION",
            Self::BadRequest { .. } => "BAD_REQUEST",
            Self::Internal(_) => "INTERNAL_ERROR",
            Self::Proxy(_) => "PROXY_ERROR",
            Self::Config(_) => "CONFIG_ERROR",
        }
    }
}
