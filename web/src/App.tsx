import { Routes, Route } from 'react-router-dom';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import History from './pages/History';
import Config from './pages/Config';
import Metrics from './pages/Metrics';
import Trash from './pages/Trash';

function App() {
  return (
    <Routes>
      <Route path="/" element={<Layout />}>
        <Route index element={<Dashboard />} />
        <Route path="history" element={<History />} />
        <Route path="trash" element={<Trash />} />
        <Route path="config" element={<Config />} />
        <Route path="metrics" element={<Metrics />} />
      </Route>
    </Routes>
  );
}

export default App;
