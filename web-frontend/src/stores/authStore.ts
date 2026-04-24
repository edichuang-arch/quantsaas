import { create } from 'zustand';

export interface User {
  user_id: number;
  email: string;
  plan: string;
  max_instances?: number;
}

interface AuthState {
  token: string | null;
  user: User | null;
  loading: boolean;
  login: (token: string, user: User) => void;
  logout: () => void;
  setUser: (user: User | null) => void;
  setLoading: (b: boolean) => void;
}

const STORAGE_KEY = 'quantsaas.auth';

export const useAuthStore = create<AuthState>((set) => ({
  token: localStorage.getItem(STORAGE_KEY),
  user: null,
  loading: true,

  login: (token, user) => {
    localStorage.setItem(STORAGE_KEY, token);
    set({ token, user, loading: false });
  },

  logout: () => {
    localStorage.removeItem(STORAGE_KEY);
    set({ token: null, user: null, loading: false });
  },

  setUser: (user) => set({ user }),
  setLoading: (loading) => set({ loading }),
}));
