use std::net::SocketAddr;
use std::sync::Arc;

use anyhow::Result;
use tracing::{info, warn};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt, EnvFilter};

mod config;
mod connectors;
mod middleware;
mod routing;
mod server;
mod proxy;
mod error;
mod management;

use config::GatewayConfig;
use routing::RouteTable;
use server::GatewayServer;

#[tokio::main]
async fn main() -> Result<()> {
    // Initialize tracing
    tracing_subscriber::registry()
        .with(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            "nexusgate_gateway=debug,tower_http=debug".into()
        }))
        .with(tracing_subscriber::fmt::layer().json())
        .init();

    info!("Starting NexusGate Gateway v{}", env!("CARGO_PKG_VERSION"));

    // Load configuration
    let config = GatewayConfig::load()?;
    info!(
        listen_addr = %config.server.listen_addr,
        management_addr = %config.server.management_addr,
        "Configuration loaded"
    );

    // Initialize shared route table
    let route_table = Arc::new(RouteTable::new());

    // Start metrics exporter
    let metrics_handle = setup_metrics(&config)?;

    // Start management API (for internal route registration)
    let mgmt_addr = config.server.management_addr;
    let mgmt_routes = route_table.clone();
    let mgmt_handle = tokio::spawn(async move {
        if let Err(e) = management::start_management_api(mgmt_addr, mgmt_routes).await {
            warn!("Management API error: {}", e);
        }
    });

    // Start main gateway server
    let server = GatewayServer::new(config, route_table);
    let server_handle = tokio::spawn(async move {
        if let Err(e) = server.run().await {
            warn!("Gateway server error: {}", e);
        }
    });

    info!("NexusGate Gateway is ready");

    // Wait for shutdown signal
    tokio::signal::ctrl_c().await?;
    info!("Shutdown signal received, draining connections...");

    Ok(())
}

fn setup_metrics(config: &GatewayConfig) -> Result<()> {
    let builder = metrics_exporter_prometheus::PrometheusBuilder::new();
    let builder = builder
        .with_http_listener(config.server.metrics_addr);
    builder.install()?;

    info!(
        metrics_addr = %config.server.metrics_addr,
        "Prometheus metrics exporter started"
    );
    Ok(())
}
