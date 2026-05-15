import { create } from 'zustand'

const STORAGE_KEY = 'pdai-theme-mode'
let mediaQueryList = null
let mediaQueryHandler = null
let initialized = false

function getSystemTheme() {
    if (typeof window === 'undefined' || !window.matchMedia) return 'light'
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function resolveTheme(mode) {
    if (mode === 'system') return getSystemTheme()
    return mode === 'dark' ? 'dark' : 'light'
}

function applyTheme(theme) {
    if (typeof document === 'undefined') return
    document.documentElement.className = theme === 'dark' ? 'dark-theme' : 'light-theme'
}

export const useThemeStore = create((set) => ({
    // 'system', 'light' or 'dark'
    mode: 'system',
    theme: 'light',

    setMode: (mode) => {
        const nextMode = mode === 'light' || mode === 'dark' ? mode : 'system'
        if (typeof window !== 'undefined') {
            localStorage.setItem(STORAGE_KEY, nextMode)
        }
        const theme = resolveTheme(nextMode)
        applyTheme(theme)
        set({ mode: nextMode, theme })
    },

    toggle: () => {
        set((state) => {
            const nextMode = state.theme === 'dark' ? 'light' : 'dark'
            if (typeof window !== 'undefined') {
                localStorage.setItem(STORAGE_KEY, nextMode)
            }
            applyTheme(nextMode)
            return { mode: nextMode, theme: nextMode }
        })
    },

    syncSystemTheme: () => {
        set((state) => {
            if (state.mode !== 'system') return state
            const theme = resolveTheme('system')
            applyTheme(theme)
            return { theme }
        })
    },

    init: () => {
        if (typeof window === 'undefined') return
        const saved = localStorage.getItem(STORAGE_KEY) || localStorage.getItem('pdai-theme') || 'system'
        localStorage.setItem(STORAGE_KEY, saved)
        const theme = resolveTheme(saved)
        applyTheme(theme)
        set({ mode: saved, theme })

        if (!initialized && window.matchMedia) {
            mediaQueryList = window.matchMedia('(prefers-color-scheme: dark)')
            mediaQueryHandler = () => {
                const currentMode = useThemeStore.getState().mode
                if (currentMode !== 'system') return
                const nextTheme = resolveTheme('system')
                applyTheme(nextTheme)
                set({ theme: nextTheme })
            }
            if (mediaQueryList.addEventListener) {
                mediaQueryList.addEventListener('change', mediaQueryHandler)
            } else if (mediaQueryList.addListener) {
                mediaQueryList.addListener(mediaQueryHandler)
            }
            initialized = true
        }
    },
}))
