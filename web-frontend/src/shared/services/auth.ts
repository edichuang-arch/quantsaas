import { api } from './apiClient';
import type { User } from '@/stores/authStore';

export interface TokenResponse {
  token: string;
  user_id: number;
  email: string;
  plan: string;
}

export const authService = {
  register: (email: string, password: string) =>
    api.post<TokenResponse>('/api/v1/auth/register', { email, password }),

  login: (email: string, password: string) =>
    api.post<TokenResponse>('/api/v1/auth/login', { email, password }),

  me: () => api.get<User>('/api/v1/auth/me'),
};
