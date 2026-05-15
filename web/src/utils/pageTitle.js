export const DEFAULT_PAGE_TITLE = '派达面板 - PDai.TOP'

export function formatPageTitle(siteName) {
    const name = String(siteName || '').trim()
    return name ? `${name} - PDai.TOP` : DEFAULT_PAGE_TITLE
}

export function applyPageTitle(siteName) {
    document.title = formatPageTitle(siteName)
}
