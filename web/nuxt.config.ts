export default defineNuxtConfig({
  ssr: false,
  compatibilityDate: '2026-07-01',
  modules: ['@nuxtjs/tailwindcss', '@pinia/nuxt'],
  runtimeConfig: {
    public: {
      apiBase: process.env.NUXT_PUBLIC_API_BASE || 'http://localhost:18081'
    }
  },
  css: ['~/assets/css/main.css']
})
