import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from './client';

export const useMetricsSummary = () =>
  useQuery({ queryKey: ['metrics-summary'], queryFn: api.getMetricsSummary, refetchInterval: 30000 });

export const useSourceConnections = () =>
  useQuery({ queryKey: ['source-connections'], queryFn: api.getSourceConnections });

export const useSourceTasks = (page = 1, perPage = 20) =>
  useQuery({ queryKey: ['source-tasks', page, perPage], queryFn: () => api.getSourceTasks(page, perPage) });

export const useSourceTask = (id: string) =>
  useQuery({ queryKey: ['source-task', id], queryFn: () => api.getSourceTask(id), enabled: !!id });

export const useSourceTaskSnapshots = (id: string) =>
  useQuery({ queryKey: ['source-task-snapshots', id], queryFn: () => api.getSourceTaskSnapshots(id), enabled: !!id });

export const useJobs = (page = 1, perPage = 20, status?: string) =>
  useQuery({ queryKey: ['jobs', page, perPage, status], queryFn: () => api.getJobs(page, perPage, status) });

export const useJob = (id: string) =>
  useQuery({ queryKey: ['job', id], queryFn: () => api.getJob(id), enabled: !!id });

export const useRetryJob = () => {
  const qc = useQueryClient();
  return useMutation({ mutationFn: api.retryJob, onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs'] }) });
};

export const useProposals = (page = 1, perPage = 20, status?: string) =>
  useQuery({ queryKey: ['proposals', page, perPage, status], queryFn: () => api.getProposals(page, perPage, status) });

export const useProposal = (id: string) =>
  useQuery({ queryKey: ['proposal', id], queryFn: () => api.getProposal(id), enabled: !!id });

export const useApproveProposal = () => {
  const qc = useQueryClient();
  return useMutation({ mutationFn: api.approveProposal, onSuccess: () => qc.invalidateQueries({ queryKey: ['proposals'] }) });
};

export const useRejectProposal = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) => api.rejectProposal(id, reason),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['proposals'] }),
  });
};

export const useProcessorRuns = (page = 1, perPage = 20) =>
  useQuery({ queryKey: ['processor-runs', page, perPage], queryFn: () => api.getProcessorRuns(page, perPage) });

export const useProcessorRun = (id: string) =>
  useQuery({ queryKey: ['processor-run', id], queryFn: () => api.getProcessorRun(id), enabled: !!id });
