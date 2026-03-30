export interface SourceConnection {
  id: string;
  provider_type: string;
  display_name: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface SourceTask {
  id: string;
  source_connection_id: string;
  external_id: string;
  entity_type: string;
  current_hash: string;
  current_status: string;
  last_seen_at: string;
  created_at: string;
  updated_at: string;
}

export interface SourceTaskSnapshot {
  id: string;
  source_task_id: string;
  snapshot_version: number;
  content_hash: string;
  detected_change_type: string;
  fetched_at: string;
  created_at: string;
}

export interface ProcessingJob {
  id: string;
  source_task_id: string;
  job_type: string;
  priority: number;
  status: string;
  attempt_count: number;
  max_attempts: number;
  error_code?: string;
  error_message?: string;
  scheduled_at: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ProcessingRun {
  id: string;
  processing_job_id: string;
  processor_name: string;
  processor_version: string;
  prompt_template_version?: string;
  model_provider?: string;
  model_name?: string;
  token_usage_json?: unknown;
  duration_ms: number;
  outcome: string;
  created_at: string;
}

export interface SuggestionProposal {
  id: string;
  source_task_id: string;
  proposal_type: string;
  proposal_payload_json?: unknown;
  human_review_required: boolean;
  approval_status: string;
  approved_by?: string;
  approved_at?: string;
  rejected_reason?: string;
  created_at: string;
  updated_at: string;
}

export interface MetricsSummary {
  total_tasks: number;
  changed_tasks_today: number;
  queued_jobs: number;
  running_jobs: number;
  failed_jobs: number;
  pending_approvals: number;
  writeback_success: number;
  writeback_failure: number;
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  per_page: number;
}
