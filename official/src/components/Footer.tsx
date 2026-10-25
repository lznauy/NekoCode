"use client";

import { motion, useReducedMotion } from "motion/react";

export function Footer() {
  const reduce = useReducedMotion();

  return (
    <footer className="relative z-10 border-t border-[var(--border)]">
      <div className="glow-line" />
      <div className="max-w-7xl mx-auto px-6 py-16">
        <motion.div
          initial={reduce ? false : { opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true }}
          transition={{ duration: 0.6 }}
          className="flex flex-col md:flex-row items-center justify-between gap-8"
        >
          <div className="flex items-center gap-3">
            <span className="text-2xl">🐱</span>
            <div>
              <span className="font-bold text-lg">NekoCode</span>
              <p className="text-sm text-[var(--muted)]">
                Terminal AI Assistant · MIT License
              </p>
            </div>
          </div>

          <div className="flex items-center gap-6 text-sm text-[var(--muted)]">
            <a
              href="https://github.com/lznauy/NekoCode"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-[var(--foreground)] transition-colors"
            >
              GitHub
            </a>
            <a
              href="https://github.com/lznauy/NekoCode/blob/master/docs/maintainers/ARCHITECTURE.md"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-[var(--foreground)] transition-colors"
            >
              Docs
            </a>
            <a
              href="https://github.com/lznauy/NekoCode/issues"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-[var(--foreground)] transition-colors"
            >
              Issues
            </a>
          </div>
        </motion.div>

        <div className="mt-12 pt-8 border-t border-[var(--border)] text-center text-xs text-[var(--muted)]">
          <p>Built with Go · Bubble Tea · Wails · React</p>
        </div>
      </div>
    </footer>
  );
}
