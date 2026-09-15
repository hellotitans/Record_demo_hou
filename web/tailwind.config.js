/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // 数据派：冷蓝理性
        data: {
          DEFAULT: '#3b82f6',
          soft: '#dbeafe',
          deep: '#1e3a8a',
        },
        // 生活派：暖橙感性
        life: {
          DEFAULT: '#f97316',
          soft: '#ffedd5',
          deep: '#7c2d12',
        },
      },
      fontFamily: {
        sans: ['"PingFang SC"', '"Microsoft YaHei"', 'system-ui', 'sans-serif'],
      },
    },
  },
  plugins: [],
}
