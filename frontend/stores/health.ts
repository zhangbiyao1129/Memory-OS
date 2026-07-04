import { defineStore } from 'pinia'

export const useHealthStore = defineStore('health', {
  state: () => ({
    lastStatus: 'unknown'
  }),
  actions: {
    mark(status: string) {
      this.lastStatus = status
    }
  }
})
