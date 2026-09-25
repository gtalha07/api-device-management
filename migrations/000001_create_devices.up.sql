CREATE TABLE IF NOT EXISTS devices (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL CHECK (btrim(name)  <> ''),
    brand      text        NOT NULL CHECK (btrim(brand) <> ''),
    state      text        NOT NULL DEFAULT 'available'
                           CHECK (state IN ('available', 'in-use', 'inactive')),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- The list endpoint filters by brand and by state; these indexes make those lookups fast.
CREATE INDEX IF NOT EXISTS idx_devices_brand ON devices (brand);
CREATE INDEX IF NOT EXISTS idx_devices_state ON devices (state);
