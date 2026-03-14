-- NexusGate Database Schema

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Service registry
CREATE TABLE services (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL UNIQUE,
    protocol VARCHAR(50) NOT NULL,
    base_url TEXT NOT NULL,
    manifest JSONB,
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Contracts
CREATE TABLE contracts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    version VARCHAR(50) NOT NULL,
    description TEXT,
    consumer_id VARCHAR(255) NOT NULL,
    status VARCHAR(20) DEFAULT 'draft',
    spec JSONB NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

CREATE INDEX idx_contracts_consumer ON contracts(consumer_id);
CREATE INDEX idx_contracts_status ON contracts(status);

-- Routes (mirrors the in-memory route table for persistence)
CREATE TABLE routes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    path_pattern TEXT NOT NULL,
    methods TEXT[] NOT NULL,
    service_id UUID REFERENCES services(id),
    contract_id UUID REFERENCES contracts(id),
    target_url TEXT NOT NULL,
    protocol VARCHAR(50) NOT NULL,
    config JSONB DEFAULT '{}',
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Consumers
CREATE TABLE consumers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    external_id VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(255),
    email VARCHAR(255),
    api_key_hash VARCHAR(255),
    status VARCHAR(20) DEFAULT 'active',
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- RBAC
CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    permissions JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE role_assignments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    consumer_id UUID REFERENCES consumers(id),
    role_id UUID REFERENCES roles(id),
    contract_id UUID REFERENCES contracts(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(consumer_id, role_id, contract_id)
);

-- Audit log
CREATE TABLE audit_log (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    consumer_id VARCHAR(255),
    action VARCHAR(100) NOT NULL,
    resource VARCHAR(255),
    contract_id UUID,
    route_id UUID,
    status VARCHAR(20),
    details JSONB,
    source_ip INET,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_audit_consumer ON audit_log(consumer_id);
CREATE INDEX idx_audit_created ON audit_log(created_at);

-- Seed default roles
INSERT INTO roles (name, description, permissions) VALUES
('admin', 'Full system access', '[{"resource": "*", "actions": ["*"]}]'),
('consumer', 'API consumer with contract-bound access', '[{"resource": "contracts:own", "actions": ["read"]}, {"resource": "routes:contracted", "actions": ["invoke"]}]'),
('readonly', 'Read-only access', '[{"resource": "capabilities:*", "actions": ["read"]}, {"resource": "contracts:own", "actions": ["read"]}]');
