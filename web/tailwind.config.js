/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        bg: "#0b0f16",
        panel: "#121a28",
        panel2: "#0f1520",
        line: "#233149",
        ink: "#e8eef8",
        muted: "#93a3bb",
        accent: "#4c8dff",
        ok: "#2fbf71",
        warn: "#f2b23c",
        bad: "#ef5a6a",
      },
      fontFamily: {
        sans: ['-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', 'Roboto', 'Helvetica', 'Arial', 'sans-serif'],
        mono: ['ui-monospace', 'Consolas', 'monospace'],
      },
    },
  },
  plugins: [],
};
