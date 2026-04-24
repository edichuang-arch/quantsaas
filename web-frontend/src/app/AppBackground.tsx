import { motion, useReducedMotion } from 'framer-motion';
import { useMemo } from 'react';

// 宇宙暗夜终端的全局动态背景：
//   - 三色大光晕（珊瑚 / 天空蓝 / 青绿）mix-blend-screen 叠加
//   - 浮动白色粒子（20 个）
//   - 噪点纹理（SVG fractal noise）
//   - 两个几何装饰形状（五边形 + 平行四边形）
// prefers-reduced-motion 时降级为 StaticBackdrop。
export function AppBackground() {
  const reduceMotion = useReducedMotion();
  if (reduceMotion) return <StaticBackdrop />;
  return <AnimatedBackdrop />;
}

function AnimatedBackdrop() {
  const particles = useMemo(
    () => Array.from({ length: 20 }, (_, i) => ({
      id: i,
      x: Math.random() * 100,
      delay: Math.random() * 10,
      duration: 15 + Math.random() * 15,
    })),
    []
  );

  return (
    <div aria-hidden className="pointer-events-none fixed inset-0 -z-10 overflow-hidden">
      {/* 三色光晕 */}
      <motion.div
        className="absolute rounded-full blur-[120px]"
        style={{
          background: '#ff8c6b',
          width: 620, height: 620,
          top: -180, left: -180,
          mixBlendMode: 'screen',
          opacity: 0.35,
        }}
        animate={{ x: [0, 60, 0], y: [0, 40, 0] }}
        transition={{ duration: 18, repeat: Infinity, ease: 'easeInOut' }}
      />
      <motion.div
        className="absolute rounded-full blur-[140px]"
        style={{
          background: '#0ea5e9',
          width: 720, height: 720,
          bottom: -220, right: -220,
          mixBlendMode: 'screen',
          opacity: 0.3,
        }}
        animate={{ x: [0, -50, 0], y: [0, -30, 0] }}
        transition={{ duration: 22, repeat: Infinity, ease: 'easeInOut' }}
      />
      <motion.div
        className="absolute rounded-full blur-[100px]"
        style={{
          background: '#2dd4bf',
          width: 520, height: 520,
          top: '30%', left: '40%',
          mixBlendMode: 'screen',
          opacity: 0.22,
        }}
        animate={{ scale: [1, 1.08, 1], opacity: [0.22, 0.3, 0.22] }}
        transition={{ duration: 14, repeat: Infinity, ease: 'easeInOut' }}
      />

      {/* 浮动粒子 */}
      {particles.map((p) => (
        <motion.div
          key={p.id}
          className="absolute h-1 w-1 rounded-full bg-white/40"
          style={{ left: `${p.x}%`, bottom: -8 }}
          animate={{ y: [-10, -1000], opacity: [0, 0.7, 0] }}
          transition={{
            duration: p.duration,
            delay: p.delay,
            repeat: Infinity,
            ease: 'linear',
          }}
        />
      ))}

      {/* 噪点 */}
      <svg className="absolute inset-0 h-full w-full opacity-[0.15] mix-blend-color-dodge">
        <filter id="noise">
          <feTurbulence baseFrequency="0.85" numOctaves="2" seed="2" />
        </filter>
        <rect width="100%" height="100%" filter="url(#noise)" />
      </svg>

      {/* 几何装饰：五边形 */}
      <motion.svg
        className="absolute left-[8%] top-[15%] h-32 w-32"
        viewBox="0 0 100 100"
        animate={{ rotate: [0, 360] }}
        transition={{ duration: 60, repeat: Infinity, ease: 'linear' }}
      >
        <polygon
          points="50,5 95,35 78,88 22,88 5,35"
          fill="url(#pentaGrad)"
          stroke="#2dd4bf"
          strokeOpacity="0.4"
          strokeWidth="1"
        />
        <defs>
          <linearGradient id="pentaGrad" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stopColor="#2dd4bf" stopOpacity="0.1" />
            <stop offset="100%" stopColor="#2dd4bf" stopOpacity="0" />
          </linearGradient>
        </defs>
      </motion.svg>

      {/* 平行四边形 */}
      <motion.svg
        className="absolute right-[10%] bottom-[18%] h-24 w-40"
        viewBox="0 0 160 100"
        animate={{ y: [0, -15, 0] }}
        transition={{ duration: 10, repeat: Infinity, ease: 'easeInOut' }}
      >
        <polygon
          points="20,10 150,10 140,90 10,90"
          fill="url(#paraGrad)"
          stroke="#ff8c6b"
          strokeOpacity="0.35"
          strokeWidth="1"
        />
        <defs>
          <linearGradient id="paraGrad" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stopColor="#ff8c6b" stopOpacity="0.12" />
            <stop offset="100%" stopColor="#ff8c6b" stopOpacity="0" />
          </linearGradient>
        </defs>
      </motion.svg>
    </div>
  );
}

function StaticBackdrop() {
  return (
    <div aria-hidden className="pointer-events-none fixed inset-0 -z-10">
      <div
        className="absolute inset-0"
        style={{
          background:
            'radial-gradient(ellipse at 20% 20%, rgba(255,140,107,0.25), transparent 50%), radial-gradient(ellipse at 80% 80%, rgba(14,165,233,0.25), transparent 50%), radial-gradient(ellipse at 50% 50%, rgba(45,212,191,0.15), transparent 60%)',
        }}
      />
    </div>
  );
}
