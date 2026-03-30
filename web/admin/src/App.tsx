import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Layout } from './components/Layout';
import { Dashboard } from './pages/Dashboard';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30_000,
    },
  },
});

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="tasks" element={<PlaceholderPage title="Source Tasks" />} />
            <Route path="tasks/:id" element={<PlaceholderPage title="Task Detail" />} />
            <Route path="jobs" element={<PlaceholderPage title="Jobs" />} />
            <Route path="jobs/:id" element={<PlaceholderPage title="Job Detail" />} />
            <Route path="proposals" element={<PlaceholderPage title="Proposals" />} />
            <Route path="proposals/:id" element={<PlaceholderPage title="Proposal Detail" />} />
            <Route path="processor-runs" element={<PlaceholderPage title="Processor Runs" />} />
            <Route path="processor-runs/:id" element={<PlaceholderPage title="Processor Run Detail" />} />
            <Route path="settings" element={<PlaceholderPage title="Settings" />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

function PlaceholderPage({ title }: { title: string }) {
  return (
    <div className="p-8">
      <h2 className="text-2xl font-bold text-gray-900 mb-2">{title}</h2>
      <p className="text-gray-500">This page is under construction.</p>
    </div>
  );
}

export default App;
