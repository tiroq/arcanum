-- Rollback migration 000046: Drop financial truth tables (Iteration 42).

DROP TABLE IF EXISTS agent_financial_matches;
DROP TABLE IF EXISTS agent_financial_facts;
DROP TABLE IF EXISTS agent_financial_events;
