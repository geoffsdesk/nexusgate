use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::SystemTime;

use dashmap::DashMap;
use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// A single route entry mapping a path pattern to a backend target.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Route {
    pub id: Uuid,
    pub path_pattern: String,
    pub methods: Vec<String>,
    pub target: RouteTarget,
    pub contract_id: Option<Uuid>,
    pub metadata: RouteMetadata,
    pub middleware: Vec<String>,
    pub enabled: bool,
    pub created_at: chrono::DateTime<chrono::Utc>,
    pub updated_at: chrono::DateTime<chrono::Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RouteTarget {
    pub service_name: String,
    pub upstream_url: String,
    pub protocol: BackendProtocol,
    pub timeout_ms: Option<u64>,
    pub retries: Option<u32>,
    pub strip_prefix: Option<String>,
    pub rewrite_path: Option<String>,
    pub headers: HashMap<String, String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum BackendProtocol {
    Http,
    Https,
    Grpc,
    GrpcTls,
    WebSocket,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RouteMetadata {
    pub service_name: String,
    pub version: String,
    pub tags: Vec<String>,
    pub description: Option<String>,
}

/// Thread-safe, lock-free route table using DashMap.
/// Supports dynamic route registration/deregistration at runtime.
pub struct RouteTable {
    /// Routes indexed by ID for O(1) lookup
    routes_by_id: DashMap<Uuid, Route>,
    /// Routes indexed by "METHOD:path_pattern" for fast matching
    routes_by_pattern: DashMap<String, Uuid>,
    /// Request counters per route for metrics
    request_counts: DashMap<Uuid, AtomicU64>,
    /// Path matcher for parameterized routes
    matchers: std::sync::RwLock<matchit::Router<Uuid>>,
}

impl RouteTable {
    pub fn new() -> Self {
        Self {
            routes_by_id: DashMap::new(),
            routes_by_pattern: DashMap::new(),
            request_counts: DashMap::new(),
            matchers: std::sync::RwLock::new(matchit::Router::new()),
        }
    }

    /// Register a new route. Returns the route ID.
    pub fn add_route(&self, route: Route) -> Result<Uuid, String> {
        let id = route.id;

        // Register in the path matcher for each method
        {
            let mut matcher = self.matchers.write().map_err(|e| e.to_string())?;
            for method in &route.methods {
                let key = format!("{}:{}", method.to_uppercase(), &route.path_pattern);
                // matchit requires unique paths, so we build a combined key
                if let Err(e) = matcher.insert(key.clone(), id) {
                    return Err(format!("Failed to register route pattern: {}", e));
                }
                self.routes_by_pattern.insert(key, id);
            }
        }

        self.request_counts.insert(id, AtomicU64::new(0));
        self.routes_by_id.insert(id, route);

        Ok(id)
    }

    /// Remove a route by ID.
    pub fn remove_route(&self, id: &Uuid) -> Option<Route> {
        if let Some((_, route)) = self.routes_by_id.remove(id) {
            // Clean up pattern index
            for method in &route.methods {
                let key = format!("{}:{}", method.to_uppercase(), &route.path_pattern);
                self.routes_by_pattern.remove(&key);
            }
            self.request_counts.remove(id);

            // Rebuild the matcher (necessary since matchit doesn't support removal)
            self.rebuild_matcher();

            Some(route)
        } else {
            None
        }
    }

    /// Match an incoming request to a route.
    pub fn match_route(&self, method: &str, path: &str) -> Option<(Route, HashMap<String, String>)> {
        let key = format!("{}:{}", method.to_uppercase(), path);

        let matcher = self.matchers.read().ok()?;

        // Try exact match first, then parameterized
        if let Ok(matched) = matcher.at(&key) {
            let route_id = *matched.value;
            let params: HashMap<String, String> = matched
                .params
                .iter()
                .map(|(k, v)| (k.to_string(), v.to_string()))
                .collect();

            if let Some(route) = self.routes_by_id.get(&route_id) {
                if route.enabled {
                    // Increment request counter
                    if let Some(counter) = self.request_counts.get(&route_id) {
                        counter.fetch_add(1, Ordering::Relaxed);
                    }
                    return Some((route.clone(), params));
                }
            }
        }

        // Try wildcard/catch-all matching
        for method_key in ["ANY", method.to_uppercase().as_str()] {
            let wildcard_key = format!("{}:{}", method_key, path);
            if let Ok(matched) = matcher.at(&wildcard_key) {
                let route_id = *matched.value;
                if let Some(route) = self.routes_by_id.get(&route_id) {
                    if route.enabled {
                        let params: HashMap<String, String> = matched
                            .params
                            .iter()
                            .map(|(k, v)| (k.to_string(), v.to_string()))
                            .collect();
                        return Some((route.clone(), params));
                    }
                }
            }
        }

        None
    }

    /// Get all registered routes.
    pub fn list_routes(&self) -> Vec<Route> {
        self.routes_by_id
            .iter()
            .map(|entry| entry.value().clone())
            .collect()
    }

    /// Get a route by ID.
    pub fn get_route(&self, id: &Uuid) -> Option<Route> {
        self.routes_by_id.get(id).map(|r| r.clone())
    }

    /// Get request count for a route.
    pub fn get_request_count(&self, id: &Uuid) -> u64 {
        self.request_counts
            .get(id)
            .map(|c| c.load(Ordering::Relaxed))
            .unwrap_or(0)
    }

    /// Update an existing route.
    pub fn update_route(&self, id: &Uuid, updated: Route) -> Result<(), String> {
        if self.routes_by_id.contains_key(id) {
            self.remove_route(id);
            self.add_route(updated)?;
            Ok(())
        } else {
            Err("Route not found".to_string())
        }
    }

    /// Rebuild the matchit router from current routes.
    fn rebuild_matcher(&self) {
        let mut new_matcher = matchit::Router::new();
        for entry in self.routes_by_id.iter() {
            let route = entry.value();
            for method in &route.methods {
                let key = format!("{}:{}", method.to_uppercase(), &route.path_pattern);
                let _ = new_matcher.insert(key, route.id);
            }
        }
        if let Ok(mut matcher) = self.matchers.write() {
            *matcher = new_matcher;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_route(path: &str, methods: Vec<&str>) -> Route {
        Route {
            id: Uuid::new_v4(),
            path_pattern: path.to_string(),
            methods: methods.into_iter().map(String::from).collect(),
            target: RouteTarget {
                service_name: "test-service".to_string(),
                upstream_url: "http://localhost:3000".to_string(),
                protocol: BackendProtocol::Http,
                timeout_ms: Some(5000),
                retries: Some(3),
                strip_prefix: None,
                rewrite_path: None,
                headers: HashMap::new(),
            },
            contract_id: None,
            metadata: RouteMetadata {
                service_name: "test".to_string(),
                version: "1.0".to_string(),
                tags: vec![],
                description: None,
            },
            middleware: vec![],
            enabled: true,
            created_at: chrono::Utc::now(),
            updated_at: chrono::Utc::now(),
        }
    }

    #[test]
    fn test_add_and_match_route() {
        let table = RouteTable::new();
        let route = test_route("GET:/api/users/{id}", vec!["GET"]);
        let id = route.id;

        table.add_route(route).unwrap();

        let result = table.match_route("GET", "GET:/api/users/123");
        assert!(result.is_some());

        let (matched, params) = result.unwrap();
        assert_eq!(matched.id, id);
        assert_eq!(params.get("id"), Some(&"123".to_string()));
    }

    #[test]
    fn test_remove_route() {
        let table = RouteTable::new();
        let route = test_route("GET:/api/test", vec!["GET"]);
        let id = route.id;

        table.add_route(route).unwrap();
        assert!(table.get_route(&id).is_some());

        table.remove_route(&id);
        assert!(table.get_route(&id).is_none());
    }

    #[test]
    fn test_list_routes() {
        let table = RouteTable::new();
        table.add_route(test_route("GET:/api/a", vec!["GET"])).unwrap();
        table.add_route(test_route("POST:/api/b", vec!["POST"])).unwrap();

        let routes = table.list_routes();
        assert_eq!(routes.len(), 2);
    }
}
