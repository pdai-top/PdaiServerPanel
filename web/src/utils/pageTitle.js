export const DEFAULT_PAGE_TITLE = '派达 - PDai.TOP'

export function formatPageTitle(siteName) {
    const name = String(siteName || '').trim()
    return name ? `${name} - 派达[pdai.top]` : DEFAULT_PAGE_TITLE
}

export function applyPageTitle(siteName) {
    document.title = formatPageTitle(siteName)
}
