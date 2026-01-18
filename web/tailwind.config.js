/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        // Custom status colors
        'status-starting': '#fbbf24', // yellow-400
        'status-ready': '#22c55e',    // green-500
        'status-running': '#3b82f6',  // blue-500
        'status-stopping': '#f97316', // orange-500
        'status-stopped': '#6b7280',  // gray-500
        'status-error': '#ef4444',    // red-500
      },
      animation: {
        'pulse-slow': 'pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
      },
    },
  },
  plugins: [],
}
