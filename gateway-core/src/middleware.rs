use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};

use dashmap::DashMap;
use serde::{Deserialize, Serialize};
use tracing::{info, warn};

// ─── Rate Limiter ────────────────────────────────────────────

/// Token bucket rate limiter with per-consumer tracking.
pub struct RateLimiter {
    buckets: DashMap<String, TokenBucket>,
    default_rate: u64,
    default_burst: u64,
}

struct TokenBucket {
    tokens: f64,
    max_tokens: f64,
    refill_rate: f64, // tokens per second
    last_refill: Instant,
}

impl RateLimiter {
    pub fn new(default_rate: u64, default_burst: u64) -> Self {
        Self {
            buckets: DashMap::new(),
            default_rate,
            default_burst,
        }
    }

    /// Check if a request should be allowed. Returns remaining tokens.
    pub fn check(&self, consumer_id: &str) -> Result<u64, Duration> {
        let mut bucket = self.buckets.entry(consumer_id.to_string()).or_insert_with(|| {
            TokenBucket {
                tokens: self.default_burst as f64,
                max_tokens: self.default_burst as f64,
                refill_rate: self.default_rate as f64,
                last_refill: Instant::now(),
            }
        });

        // Refill tokens based on elapsed time
        let now = Instant::now();
        let elapsed = now.duration_since(bucket.last_refill).as_secs_f64();
        bucket.tokens = (bucket.tokens + elapsed * bucket.refill_rate).min(bucket.max_tokens);
        bucket.last_refill = now;

        if bucket.tokens >= 1.0 {
            bucket.tokens -= 1.0;
            Ok(bucket.tokens as u64)
        } else {
            // Calculate retry-after duration
            let wait_secs = (1.0 - bucket.tokens) / bucket.refill_rate;
            Err(Duration::from_secs_f64(wait_secs))
        }
    }

    /// Set custom rate limits for a specific consumer.
    pub fn set_consumer_limits(&self, consumer_id: &str, rate: u64, burst: u64) {
        self.buckets.insert(
            consumer_id.to_string(),
            TokenBucket {
                tokens: burst as f64,
                max_tokens: burst as f64,
                refill_rate: rate as f64,
                last_refill: Instant::now(),
            },
        );
    }
}

// ─── Circuit Breaker ─────────────────────────────────────────

#[derive(Debug, Clone, Copy, PartialEq, Serialize)]
pub enum CircuitState {
    Closed,
    Open,
    HalfOpen,
}

pub struct CircuitBreaker {
    circuits: DashMap<String, CircuitData>,
    failure_threshold: u32,
    success_threshold: u32,
    timeout: Duration,
}

struct CircuitData {
    state: CircuitState,
    failure_count: u32,
    success_count: u32,
    last_failure: Option<Instant>,
    last_state_change: Instant,
}

impl CircuitBreaker {
    pub fn new(failure_threshold: u32, success_threshold: u32, timeout: Duration) -> Self {
        Self {
            circuits: DashMap::new(),
            failure_threshold,
            success_threshold,
            timeout,
        }
    }

    /// Check if requests to a service should be allowed.
    pub fn can_request(&self, service: &str) -> Result<(), CircuitState> {
        let mut circuit = self.circuits.entry(service.to_string()).or_insert_with(|| {
            CircuitData {
                state: CircuitState::Closed,
                failure_count: 0,
                success_count: 0,
                last_failure: None,
                last_state_change: Instant::now(),
            }
        });

        match circuit.state {
            CircuitState::Closed => Ok(()),
            CircuitState::Open => {
                // Check if timeout has elapsed → transition to half-open
                if circuit.last_state_change.elapsed() >= self.timeout {
                    circuit.state = CircuitState::HalfOpen;
                    circuit.success_count = 0;
                    circuit.last_state_change = Instant::now();
                    info!(service = service, "Circuit breaker → HalfOpen");
                    Ok(())
                } else {
                    Err(CircuitState::Open)
                }
            }
            CircuitState::HalfOpen => Ok(()), // Allow probe requests
        }
    }

    /// Record a successful request.
    pub fn record_success(&self, service: &str) {
        if let Some(mut circuit) = self.circuits.get_mut(service) {
            match circuit.state {
                CircuitState::HalfOpen => {
                    circuit.success_count += 1;
                    if circuit.success_count >= self.success_threshold {
                        circuit.state = CircuitState::Closed;
                        circuit.failure_count = 0;
                        circuit.last_state_change = Instant::now();
                        info!(service = service, "Circuit breaker → Closed");
                    }
                }
                CircuitState::Closed => {
                    circuit.failure_count = 0; // Reset on success
                }
                _ => {}
            }
        }
    }

    /// Record a failed request.
    pub fn record_failure(&self, service: &str) {
        let mut circuit = self.circuits.entry(service.to_string()).or_insert_with(|| {
            CircuitData {
                state: CircuitState::Closed,
                failure_count: 0,
                success_count: 0,
                last_failure: None,
                last_state_change: Instant::now(),
            }
        });

        circuit.failure_count += 1;
        circuit.last_failure = Some(Instant::now());

        match circuit.state {
            CircuitState::Closed => {
                if circuit.failure_count >= self.failure_threshold {
                    circuit.state = CircuitState::Open;
                    circuit.last_state_change = Instant::now();
                    warn!(
                        service = service,
                        failures = circuit.failure_count,
                        "Circuit breaker → Open"
                    );
                }
            }
            CircuitState::HalfOpen => {
                // Any failure in half-open goes back to open
                circuit.state = CircuitState::Open;
                circuit.last_state_change = Instant::now();
                warn!(service = service, "Circuit breaker → Open (from HalfOpen)");
            }
            _ => {}
        }
    }

    /// Get current state for a service.
    pub fn get_state(&self, service: &str) -> CircuitState {
        self.circuits
            .get(service)
            .map(|c| c.state)
            .unwrap_or(CircuitState::Closed)
    }
}

// ─── Request/Response Transform ──────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransformRule {
    pub add_headers: HashMap<String, String>,
    pub remove_headers: Vec<String>,
    pub rewrite_path: Option<(String, String)>, // (from, to) regex
}

impl TransformRule {
    pub fn apply_request_headers(
        &self,
        headers: &mut hyper::HeaderMap,
    ) {
        // Remove specified headers
        for name in &self.remove_headers {
            if let Ok(header_name) = hyper::header::HeaderName::from_bytes(name.as_bytes()) {
                headers.remove(&header_name);
            }
        }

        // Add specified headers
        for (name, value) in &self.add_headers {
            if let (Ok(header_name), Ok(header_value)) = (
                hyper::header::HeaderName::from_bytes(name.as_bytes()),
                hyper::header::HeaderValue::from_str(value),
            ) {
                headers.insert(header_name, header_value);
            }
        }
    }
}

// ─── Middleware Pipeline ─────────────────────────────────────

/// Represents a processing step in the middleware chain.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum MiddlewareConfig {
    RateLimit { requests_per_second: u64, burst: u64 },
    CircuitBreaker { failure_threshold: u32, recovery_timeout_ms: u64 },
    Transform(TransformRule),
    Authentication { provider: String },
    Logging { level: String },
    Cors { allowed_origins: Vec<String> },
    Compression { algorithms: Vec<String> },
    Timeout { ms: u64 },
}

/// The middleware pipeline aggregates all middleware for a route.
pub struct MiddlewarePipeline {
    pub rate_limiter: Arc<RateLimiter>,
    pub circuit_breaker: Arc<CircuitBreaker>,
    pub transforms: Vec<TransformRule>,
}

impl MiddlewarePipeline {
    pub fn new(rate_limiter: Arc<RateLimiter>, circuit_breaker: Arc<CircuitBreaker>) -> Self {
        Self {
            rate_limiter,
            circuit_breaker,
            transforms: Vec::new(),
        }
    }
}
