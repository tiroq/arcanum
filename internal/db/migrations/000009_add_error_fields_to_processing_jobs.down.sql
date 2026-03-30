ALTER TABLE processing_jobs
    DROP COLUMN IF EXISTS error_code,
    DROP COLUMN IF EXISTS error_message;
