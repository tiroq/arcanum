-- Rollback migration 000032: Remove mode-specific calibration tables.

DROP TABLE IF EXISTS agent_mode_calibration_summary;
DROP TABLE IF EXISTS agent_mode_calibration_records;
