ALTER TABLE processing_jobs
    ADD COLUMN IF NOT EXISTS error_code    TEXT,
    ADD COLUMN IF NOT EXISTS error_message TEXT;
