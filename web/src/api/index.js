import axios from 'axios'

const api = axios.create({
    baseURL: '/api',
    timeout: 15000,
    headers: { 'Content-Type': 'application/json' },
})

// Attach JWT token to every request
api.interceptors.request.use((config) => {
    const token = localStorage.getItem('token')
    if (token) {
        config.headers.Authorization = `Bearer ${token}`
    }
    return config
})

// Handle 401 responses — redirect to login
api.interceptors.response.use(
    (response) => response,
    (error) => {
        if (error.response?.status === 401) {
            localStorage.removeItem('token')
            window.location.href = '/login'
        }
        return Promise.reject(error)
    }
)

/**
 * Build the Sec-WebSocket-Protocol array that authenticates a WebSocket
 * handshake. Pass this as the second argument to `new WebSocket(url, protocols)`.
 * Prefer this over `?token=<jwt>` in the URL — subprotocols are not typically
 * logged by reverse proxies and don't appear in devtools URL bars.
 *
 * Returns [] when no token is stored so callers that forgot to guard still
 * trigger a 401 at auth time rather than opening an unauthenticated socket.
 */
export function wsAuthProtocols() {
    const token = localStorage.getItem('token')
    return token ? [`pdai.token.${token}`] : []
}

/**
 * Stream a Server-Sent Events endpoint with proper Authorization header.
 * Browser-native EventSource doesn't support custom headers, so we use
 * fetch + ReadableStream + a small SSE parser instead. Avoids dragging
 * in event-source-polyfill or stuffing the JWT into the URL.
 *
 * Callbacks:
 *   onLog(line)     — fired for each `event: log` data line
 *   onStatus(s)     — fired for each `event: status` data line
 *   onDone(final)   — fired once on `event: done`; stream is closed
 *   onError(err)    — fired on network/HTTP error; stream is closed
 *
 * Returns an AbortController; call .abort() to close early.
 */
export function streamSSE(url, { onLog, onStatus, onReset, onDone, onError } = {}) {
    const ctrl = new AbortController()
    const token = localStorage.getItem('token')
        ; (async () => {
            try {
                const resp = await fetch(url, {
                    headers: token ? { Authorization: `Bearer ${token}` } : {},
                    signal: ctrl.signal,
                })
                if (!resp.ok) {
                    onError?.(new Error(`stream HTTP ${resp.status}`))
                    return
                }
                const reader = resp.body.getReader()
                const decoder = new TextDecoder()
                let buf = ''
                while (true) {
                    const { done, value } = await reader.read()
                    if (done) break
                    buf += decoder.decode(value, { stream: true })
                    let nl
                    while ((nl = buf.indexOf('\n\n')) !== -1) {
                        const block = buf.slice(0, nl)
                        buf = buf.slice(nl + 2)
                        let event = 'message'
                        let data = ''
                        for (const line of block.split('\n')) {
                            if (line.startsWith('event: ')) event = line.slice(7)
                            else if (line.startsWith('data: ')) data += (data ? '\n' : '') + line.slice(6)
                        }
                        if (event === 'log') onLog?.(data)
                        else if (event === 'status') onStatus?.(data)
                        else if (event === 'reset') onReset?.(data)
                        else if (event === 'error') onError?.(new Error(data))
                        else if (event === 'done') { onDone?.(data); return }
                    }
                }
            } catch (err) {
                if (err.name !== 'AbortError') onError?.(err)
            }
        })()
    return ctrl
}

// ============ Auth ============
export const authAPI = {
    needSetup: () => api.get('/auth/need-setup'),
    setup: (data) => api.post('/auth/setup', data),
    login: (data) => api.post('/auth/login', data),
    me: () => api.get('/auth/me'),
    updateProfile: (data) => api.put('/auth/profile', data),
}

// ============ Hosts ============
export const hostAPI = {
    list: (params) => api.get('/hosts', { params }),
    get: (id) => api.get(`/hosts/${id}`),
    create: (data) => api.post('/hosts', data),
    update: (id, data) => api.put(`/hosts/${id}`, data),
    delete: (id) => api.delete(`/hosts/${id}`),
    toggle: (id) => api.patch(`/hosts/${id}/toggle`),
    clone: (id, data) => api.post(`/hosts/${id}/clone`, data),
    uploadCert: (id, formData) => api.post(`/hosts/${id}/cert`, formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
    }),
    deleteCert: (id) => api.delete(`/hosts/${id}/cert`),
}

// ============ DNS Check ============
export const dnsCheckAPI = {
    check: (domain) => api.get('/dns-check', { params: { domain } }),
}

// ============ Caddy ============
export const caddyAPI = {
    status: () => api.get('/caddy/status'),
    start: () => api.post('/caddy/start'),
    stop: () => api.post('/caddy/stop'),
    reload: () => api.post('/caddy/reload'),
    restart: () => api.post('/caddy/restart'),
    caddyfile: () => api.get('/caddy/caddyfile'),
    saveCaddyfile: (content, reload = false) => api.post('/caddy/caddyfile', { content, reload }),
    format: (content) => api.post('/caddy/fmt', { content }),
    validate: (content) => api.post('/caddy/validate', { content }),
}

// ============ Logs ============
export const logAPI = {
    get: (params) => api.get('/logs', { params }),
    files: () => api.get('/logs/files'),
    downloadUrl: (type) => `/api/logs/download?type=${type}`,
    system: (params) => api.get('/logs/system', { params }),
}

// ============ Config ============
export const configAPI = {
    export: () => api.get('/config/export'),
    import: (data) => api.post('/config/import', data),
}

// ============ Dashboard ============
export const dashboardAPI = {
    stats: () => api.get('/dashboard/stats'),
    news: () => api.get('/news'),
}

// ============ Settings ============
export const settingAPI = {
    getAll: () => api.get('/settings/all'),
    update: (key, value) => api.put('/settings', { key, value }),
}

// ============ Plugins ============
export const pluginAPI = {
    list: () => api.get('/plugins'),
    enable: (id) => api.post(`/plugins/${id}/enable`),
    disable: (id) => api.post(`/plugins/${id}/disable`),
    frontendManifests: () => api.get('/plugins/frontend-manifests'),
    setSidebarVisible: (id, visible) => api.post(`/plugins/${id}/sidebar`, { visible }),
    install: (id) => api.post(`/plugins/${id}/install`),
}

// ============ Certificates ============
export const certificateAPI = {
    list: () => api.get('/certificates'),
    upload: (formData) => api.post('/certificates', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
    }),
    delete: (id) => api.delete(`/certificates/${id}`),
}

// ============ Docker (plugin) ============
export const dockerAPI = {
    // System
    info: () => api.get('/plugins/docker/info'),
    status: () => api.get('/plugins/docker/status'),
    switchPodman: () => api.post('/plugins/docker/switch-podman', {}, { timeout: 600000 }),

    // Daemon config
    getDaemonConfig: () => api.get('/plugins/docker/daemon-config'),
    updateDaemonConfig: (data) => api.put('/plugins/docker/daemon-config', data, { timeout: 60000 }),

    // Containers
    listContainers: (all = true) => api.get('/plugins/docker/containers', { params: { all } }),
    getContainer: (id) => api.get(`/plugins/docker/containers/${id}`),
    runContainer: (data) => api.post('/plugins/docker/containers/run', data, { timeout: 300000 }),
    runContainerStreamUrl: () => '/api/plugins/docker/containers/run/stream',
    updateContainer: (id, data) => api.put(`/plugins/docker/containers/${id}`, data, { timeout: 300000 }),
    startContainer: (id) => api.post(`/plugins/docker/containers/${id}/start`),
    stopContainer: (id) => api.post(`/plugins/docker/containers/${id}/stop`),
    restartContainer: (id) => api.post(`/plugins/docker/containers/${id}/restart`),
    removeContainer: (id) => api.delete(`/plugins/docker/containers/${id}`),
    containerLogs: (id, tail) => api.get(`/plugins/docker/containers/${id}/logs`, { params: { tail } }),
    containerStats: (id) => api.get(`/plugins/docker/containers/${id}/stats`),

    // Images
    listImages: () => api.get('/plugins/docker/images'),
    pullImage: (image) => api.post('/plugins/docker/images/pull', { image }, { timeout: 300000 }),
    removeImage: (id) => api.delete(`/plugins/docker/images/${id}`),
    pruneImages: () => api.post('/plugins/docker/images/prune'),
    searchImages: (q, limit) => api.get('/plugins/docker/images/search', { params: { q, limit } }),

    // Networks
    listNetworks: () => api.get('/plugins/docker/networks'),
    createNetwork: (name) => api.post('/plugins/docker/networks', { name }),
    removeNetwork: (id) => api.delete(`/plugins/docker/networks/${id}`),

    // Volumes
    listVolumes: () => api.get('/plugins/docker/volumes'),
    createVolume: (name) => api.post('/plugins/docker/volumes', { name }),
    removeVolume: (id) => api.delete(`/plugins/docker/volumes/${id}`),
}

// ============ File Manager (plugin) ============
export const fileManagerAPI = {
    list: (path) => api.get('/plugins/filemanager/list', { params: { path } }),
    read: (path) => api.get('/plugins/filemanager/read', { params: { path } }),
    write: (path, content) => api.post('/plugins/filemanager/write', { path, content }),
    upload: (formData) => api.post('/plugins/filemanager/upload', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
        timeout: 300000,
    }),
    download: (path) => `/api/plugins/filemanager/download?path=${encodeURIComponent(path)}`,
    mkdir: (path) => api.post('/plugins/filemanager/mkdir', { path }),
    delete: (paths) => api.delete('/plugins/filemanager/delete', { data: { paths } }),
    rename: (old_path, new_path) => api.post('/plugins/filemanager/rename', { old_path, new_path }),
    chmod: (path, mode) => api.post('/plugins/filemanager/chmod', { path, mode }),
    info: (path) => api.get('/plugins/filemanager/info', { params: { path } }),
    compress: (paths, dest, format) => api.post('/plugins/filemanager/compress', { paths, dest, format }),
    extract: (path, dest) => api.post('/plugins/filemanager/extract', { path, dest }),
    terminalWsUrl: (cols, rows) => {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        return `${proto}//${window.location.host}/api/plugins/filemanager/terminal/ws?cols=${cols}&rows=${rows}`
    },
}

// ============ Database (plugin) ============
export const databaseAPI = {
    engines: () => api.get('/plugins/database/engines'),
    presets: () => api.get('/plugins/database/presets'),
    postgresTuningPresets: () => api.get('/plugins/database/presets/postgres-tuning'),

    listInstances: () => api.get('/plugins/database/instances'),
    getInstance: (id) => api.get(`/plugins/database/instances/${id}`),
    createInstance: (data) => api.post('/plugins/database/instances', data),
    deleteInstance: (id) => api.delete(`/plugins/database/instances/${id}`),
    startInstance: (id) => api.post(`/plugins/database/instances/${id}/start`),
    stopInstance: (id) => api.post(`/plugins/database/instances/${id}/stop`),
    restartInstance: (id) => api.post(`/plugins/database/instances/${id}/restart`),
    testConnection: (id) => api.post(`/plugins/database/instances/${id}/test`),
    instanceLogs: (id, tail) => api.get(`/plugins/database/instances/${id}/logs`, { params: { tail } }),
    connectionInfo: (id) => api.get(`/plugins/database/instances/${id}/connection`),
    rootPassword: (id) => api.get(`/plugins/database/instances/${id}/password`),
    listBackups: (id) => api.get(`/plugins/database/instances/${id}/backups`),
    createBackup: (id) => api.post(`/plugins/database/instances/${id}/backups`, {}, { timeout: 1800000 }),
    deleteBackup: (id, backupId) => api.delete(`/plugins/database/instances/${id}/backups/${backupId}`),
    restoreBackup: (id, backupId) => api.post(`/plugins/database/instances/${id}/backups/${backupId}/restore`, {}, { timeout: 1800000 }),
    downloadBackup: (id, backupId) => api.get(`/plugins/database/instances/${id}/backups/${backupId}/download`, { responseType: 'blob', timeout: 1800000 }),
    backupDownloadUrl: (id, backupId) => `/api/plugins/database/instances/${id}/backups/${backupId}/download`,

    listDatabases: (id) => api.get(`/plugins/database/instances/${id}/databases`),
    createDatabase: (id, data) => api.post(`/plugins/database/instances/${id}/databases`, data),
    deleteDatabase: (id, dbname) => api.delete(`/plugins/database/instances/${id}/databases/${dbname}`),

    listUsers: (id) => api.get(`/plugins/database/instances/${id}/users`),
    createUser: (id, data) => api.post(`/plugins/database/instances/${id}/users`, data),
    deleteUser: (id, username) => api.delete(`/plugins/database/instances/${id}/users/${username}`),

    sqliteTables: (path) => api.get('/plugins/database/sqlite/tables', { params: { path } }),
    sqliteSchema: (path, table) => api.get('/plugins/database/sqlite/schema', { params: { path, table } }),
    sqliteQuery: (path, query, limit) => api.post('/plugins/database/sqlite/query', { path, query, limit }),

    executeQuery: (id, data) => api.post(`/plugins/database/instances/${id}/query`, data, { timeout: 35000 }),

    instanceLogsWsUrl: (id) => {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        return `${proto}//${window.location.host}/api/plugins/database/instances/${id}/logs/ws?tail=100`
    },
}

// ============ Monitoring (plugin) ============
export const monitoringAPI = {
    getCurrent: () => api.get('/plugins/monitoring/metrics/current'),
    getHistory: (period) => api.get('/plugins/monitoring/metrics/history', { params: { period } }),
    getContainers: () => api.get('/plugins/monitoring/metrics/containers'),
    metricsWsUrl: () => {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        return `${proto}//${window.location.host}/api/plugins/monitoring/metrics/ws`
    },
    listAlertRules: () => api.get('/plugins/monitoring/alerts'),
    createAlertRule: (data) => api.post('/plugins/monitoring/alerts', data),
    updateAlertRule: (id, data) => api.put(`/plugins/monitoring/alerts/${id}`, data),
    deleteAlertRule: (id) => api.delete(`/plugins/monitoring/alerts/${id}`),
    listAlertHistory: (limit) => api.get('/plugins/monitoring/alerts/history', { params: { limit } }),
}

// ============ DNS Providers ============
export const dnsProviderAPI = {
    list: () => api.get('/dns-providers'),
    get: (id) => api.get(`/dns-providers/${id}`),
    create: (data) => api.post('/dns-providers', data),
    update: (id, data) => api.put(`/dns-providers/${id}`, data),
    delete: (id) => api.delete(`/dns-providers/${id}`),
}

// ============ Firewall (plugin) ============
export const firewallAPI = {
    status: () => api.get('/plugins/firewall/status'),
    zones: () => api.get('/plugins/firewall/zones'),
    zone: (name) => api.get(`/plugins/firewall/zones/${name}`),
    addPort: (data) => api.post('/plugins/firewall/ports', data),
    updatePort: (data) => api.put('/plugins/firewall/ports', data),
    removePort: (data) => api.delete('/plugins/firewall/ports', { data }),
    addService: (data) => api.post('/plugins/firewall/services', data),
    removeService: (data) => api.delete('/plugins/firewall/services', { data }),
    addRichRule: (data) => api.post('/plugins/firewall/rich-rules', data),
    removeRichRule: (data) => api.delete('/plugins/firewall/rich-rules', { data }),
    availableServices: () => api.get('/plugins/firewall/available-services'),
    reload: () => api.post('/plugins/firewall/reload'),
    clearRules: () => api.post('/plugins/firewall/clear-rules'),
    start: () => api.post('/plugins/firewall/start'),
}

// ── Cron Jobs ──
export const cronjobAPI = {
    listTasks: (tag) => api.get('/plugins/cronjob/tasks', { params: { tag } }),
    getTask: (id) => api.get(`/plugins/cronjob/tasks/${id}`),
    createTask: (data) => api.post('/plugins/cronjob/tasks', data),
    updateTask: (id, data) => api.put(`/plugins/cronjob/tasks/${id}`, data),
    deleteTask: (id) => api.delete(`/plugins/cronjob/tasks/${id}`),
    triggerTask: (id) => api.post(`/plugins/cronjob/tasks/${id}/trigger`),
    taskLogs: (id, limit) => api.get(`/plugins/cronjob/tasks/${id}/logs`, { params: { limit } }),
    allLogs: (limit) => api.get('/plugins/cronjob/logs', { params: { limit } }),
}

// ── Supervisor ──
export const supervisorAPI = {
    listProcesses: () => api.get('/plugins/supervisor/processes'),
    getProcess: (id) => api.get(`/plugins/supervisor/processes/${id}`),
    createProcess: (data) => api.post('/plugins/supervisor/processes', data),
    updateProcess: (id, data) => api.put(`/plugins/supervisor/processes/${id}`, data),
    deleteProcess: (id) => api.delete(`/plugins/supervisor/processes/${id}`),
    startProcess: (id) => api.post(`/plugins/supervisor/processes/${id}/start`),
    stopProcess: (id) => api.post(`/plugins/supervisor/processes/${id}/stop`),
    restartProcess: (id) => api.post(`/plugins/supervisor/processes/${id}/restart`),
    processLogs: (id, limit) => api.get(`/plugins/supervisor/processes/${id}/logs`, { params: { limit } }),
    allLogs: (limit) => api.get('/plugins/supervisor/logs', { params: { limit } }),
}

export default api
