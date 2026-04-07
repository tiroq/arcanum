CREATE TABLE IF NOT EXISTS agent_exploration_budget (
    id          INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    max_per_cycle    INTEGER NOT NULL DEFAULT 1,
    max_per_hour     INTEGER NOT NULL DEFAULT 3,
    used_this_window INTEGER NOT NULL DEFAULT 0,
    window_start     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO agent_exploration_budget (id, max_per_cycle, max_per_hour, used_this_window, window_start, updated_at)
VALUES (1, 1, 3, 0, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;
