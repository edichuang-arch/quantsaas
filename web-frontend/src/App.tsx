import { AuthProvider } from './app/AuthProvider';
import { AppRouter } from './app/router';
import { AppBackground } from './app/AppBackground';

export default function App() {
  return (
    <AuthProvider>
      <AppBackground />
      <AppRouter />
    </AuthProvider>
  );
}
