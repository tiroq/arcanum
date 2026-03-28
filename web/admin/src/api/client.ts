import type {
  MetricsSummary,
  PaginatedResponse,
  ProcessingJob,
  ProcessingRun,
  SourceConnection,
  SourceTask,
  SourceTaskSnapshot,
  SuggestionProposal,
} from './types';

const API_URL = (import.meta.env.VITE_API_URL as string) || 'http://localhost:8080';
const ADMIN_TOKEN = (import.meta.env.VITE_ADMIN_TOKEN as string) || '';

async function apiRequest<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_URL}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      'X-Admin-Token': ADMIN_TOKEN,
      ...(options?.headers || {}),
    },
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText })) as { error?: string };
    throw new Error(error.error || `HTTP ${response.status}`);
  }
  return response.json() as Promise<T>;
}

export const api = {
  getMetricsSummary: () => apiRequest<MetricsSummary>('/api/v1/metrics/summary'),
  getSourceConnections: () => apiRequest<PaginatedResponse<SourceConnection>>('/api/v1/source-connections'),
  getSourceTasks: (page = 1, perPage = 20) =>
    apiRequest<PaginatedResponse<SourceTask>>(`/api/v1/source-tasks?page=${page}&per_page=${perPage}`),
  getSourceTask: (id: string) => apiRequest<SourceTask>(`/api/v1/source-tasks/${id}`),
  getSourceTaskSnapshots: (id: string) =>
    apiRequest<PaginatedResponse<SourceTaskSnapshot>>(`/api/v1/source-tasks/${id}/snapshots`),
  getJobs: (page = 1, perPage = 20, status?: string) =>
    apiRequest<PaginatedResponse<ProcessingJob>>(
      `/api/v1/jobs?page=${page}&per_page=${perPage}${status ? `&status=${status}` : ''}`
    ),
  getJob: (id: string) => apiRequest<ProcessingJob>(`/api/v1/jobs/${id}`),
  retryJob: (id: string) => apiRequest<ProcessingJob>(`/api/v1/jobs/${id}/retry`, { method: 'POST' }),
  getProposals: (page = 1, perPage = 20, status?: string) =>
    apiRequest<PaginatedResponse<SuggestionProposal>>(
      `/api/v1/proposals?page=${page}&per_page=${perPage}${status ? `&status=${status}` : ''}`
    ),
  getProposal: (id: string) => apiRequest<SuggestionProposal>(`/api/v1/proposals/${id}`),
  approveProposal: (id: string) =>
    apiRequest<SuggestionProposal>(`/api/v1/proposals/${id}/approve`, { method: 'POST' }),
  rejectProposal: (id: string, reason: string) =>
    apiRequest<SuggestionProposal>(`/api/v1/proposals/${id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    }),
  getProcessorRuns: (page = 1, perPage = 20) =>
    apiRequest<PaginatedResponse<ProcessingRun>>(`/api/v1/processor-runs?page=${page}&per_page=${perPage}`),
  getProcessorRun: (id: string) => apiRequest<ProcessingRun>(`/api/v1/processor-runs/${id}`),
};
