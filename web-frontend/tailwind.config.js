/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        qs: {
          bg: '#020617',
          surface: 'rgba(255,255,255,0.04)',
          accent: '#2dd4bf',
          warm: '#ff8c6b',
          info: '#0ea5e9',
          safe: '#34d399',
          danger: '#f87171',
          warn: '#fbbf24',
        },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'SFMono-Regular', 'ui-monospace', 'monospace'],
      },
      boxShadow: {
        glow: '0 0 30px rgba(45, 212, 191, 0.15)',
      },
    },
  },
  plugins: [],
};
