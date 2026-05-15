import { useState, useEffect, useCallback, useRef as useReactRef } from 'react'
import {
    Box, Flex, Heading, Text, Button, Badge, Switch, Table, Dialog,
    TextField, Callout, IconButton, Card, Tooltip, Spinner, AlertDialog,
    Select, Tabs, Separator,
} from '@radix-ui/themes'
import {
    Plus, Pencil, Trash2, Globe, AlertCircle, X, ChevronRight,
    ArrowRightLeft, Shield, Lock, Copy, ExternalLink,
} from 'lucide-react'
import { hostAPI, certificateAPI, dnsProviderAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

const parseAdditionalDomains = (value) => {
    const seen = new Set()
    return String(value || '')
        .split(/[\n,]+/)
        .map((domain) => domain.trim())
        .filter((domain) => {
            if (!domain) return false
            const key = domain.toLowerCase()
            if (seen.has(key)) return false
            seen.add(key)
            return true
        })
}

const additionalDomainsToText = (domains = []) => domains
    .map((item) => typeof item === 'string' ? item : item?.domain)
    .filter(Boolean)
    .join('\n')

const hostDomainListFromForm = (source) => {
    const rawDomains = [
        ...parseAdditionalDomains(source.domain),
        ...parseAdditionalDomains(additionalDomainsToText(source.domains)),
    ]
    const primary = rawDomains[0] || source.domain?.trim() || 'example.com'
    const seen = new Set([primary.toLowerCase()])
    const domains = [primary]
    rawDomains.slice(1).forEach((domain) => {
        const key = domain.toLowerCase()
        if (!seen.has(key)) {
            seen.add(key)
            domains.push(domain)
        }
    })
    return domains
}

const DEFAULT_FORM = {
    domain: '',
    host_type: 'proxy',
    tls_enabled: true,
    http_redirect: false,
    websocket: false,
    enabled: true,
    upstreams: [{ address: '' }],
    redirect_url: '',
    redirect_code: 301,
    root_path: '',
    directory_browse: false,
    custom_headers: [],
    access_rules: [],
    basic_auths: [],
    custom_directives: '',
    full_caddy_block: '',
    compression: false,
    cors_enabled: false,
    cors_origins: '*',
    cors_methods: 'GET, POST, PUT, DELETE, OPTIONS',
    cors_headers: 'Content-Type, Authorization',
    security_headers: false,
    error_page_path: '',
    cache_enabled: false,
    cache_ttl: 300,
    tls_mode: 'auto',
    dns_provider_id: null,
}

const DEFAULT_DATA_DIR = './data'

const defaultSiteRootPath = (domain) => `${DEFAULT_DATA_DIR}/www/${domain || 'example.com'}/index`
const defaultErrorPagePath = (domain) => `${DEFAULT_DATA_DIR}/www/${domain || 'example.com'}/default`

const buildCaddyBlockFromForm = (source) => {
    const domains = hostDomainListFromForm(source)
    const domain = domains[0] || 'example.com'
    const tlsMode = source.tls_mode || 'auto'
    const tlsDisabled = tlsMode === 'off' || source.tls_enabled === false
    const shouldHttpRedirect = Boolean(source.http_redirect) && !tlsDisabled && source.host_type !== 'redirect'
    const siteAddresses = tlsDisabled
        ? domains.map((name) => `http://${name}`)
        : domains
    const lines = [`${siteAddresses.join(', ')} {`]

    if (shouldHttpRedirect) {
        lines.push('    @http protocol http')
        lines.push('    redir @http https://{host}{uri} permanent')
    }

    if (tlsMode === 'custom') {
        lines.push('    tls /path/to/cert.pem /path/to/key.pem')
    } else if (tlsMode === 'dns' || tlsMode === 'wildcard') {
        lines.push('    tls {')
        lines.push('        dns <provider> <api_token>')
        lines.push('    }')
    }

    lines.push('')
    if (source.compression) lines.push('    encode gzip zstd')

    if (source.cors_enabled) {
        const origins = source.cors_origins || '*'
        const methods = source.cors_methods || 'GET, POST, PUT, DELETE, OPTIONS'
        const headers = source.cors_headers || 'Content-Type, Authorization'
        lines.push('    @cors_preflight method OPTIONS')
        lines.push('    header @cors_preflight {')
        lines.push(`        Access-Control-Allow-Origin "${origins}"`)
        lines.push(`        Access-Control-Allow-Methods "${methods}"`)
        lines.push(`        Access-Control-Allow-Headers "${headers}"`)
        lines.push('        Access-Control-Max-Age "86400"')
        lines.push('    }')
        lines.push('    respond @cors_preflight 204')
        lines.push(`    header Access-Control-Allow-Origin "${origins}"`)
    }

    if (source.security_headers) {
        lines.push('    header {')
        lines.push('        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"')
        lines.push('        X-Content-Type-Options "nosniff"')
        lines.push('        X-Frame-Options "DENY"')
        lines.push('        Referrer-Policy "strict-origin-when-cross-origin"')
        lines.push('        X-XSS-Protection "1; mode=block"')
        lines.push('        Permissions-Policy "camera=(), microphone=(), geolocation=()"')
        lines.push('    }')
    }

    if (source.host_type === 'redirect') {
        lines.push(`    redir ${source.redirect_url || 'https://example.com'}{uri} ${Number(source.redirect_code) === 302 ? 'temporary' : 'permanent'}`)
    } else if (source.host_type === 'static') {
        lines.push(`    root * ${source.root_path || defaultSiteRootPath(domain)}`)
        lines.push(source.directory_browse ? '    file_server browse' : '    file_server')
    } else {
        const upstreams = (source.upstreams || []).map((u) => u.address.trim()).filter(Boolean)
        const upstreamText = upstreams.length ? upstreams.join(' ') : '127.0.0.1:3000'
        if (source.websocket) {
            lines.push(`    reverse_proxy ${upstreamText} {`)
            lines.push('        header_up X-Real-IP {remote_host}')
            lines.push('    }')
        } else {
            lines.push(`    reverse_proxy ${upstreamText}`)
        }
    }

    if (source.custom_directives?.trim()) {
        lines.push(...source.custom_directives.trim().split('\n').map((line) => `    ${line}`))
    }

    if (source.error_page_path?.trim()) {
        const errorPagePath = source.error_page_path.trim()
        lines.push('    handle_errors {')
        ;[404, 502, 503].forEach((code) => {
            lines.push(`        @${code} expression {err.status_code} == ${code}`)
            lines.push(`        handle @${code} {`)
            lines.push(`            root * ${errorPagePath}`)
            lines.push(`            rewrite * /${code}.html`)
            lines.push('            file_server')
            lines.push('        }')
        })
        lines.push('    }')
    }

    lines.push(
        '    log {',
        `        output file ./data/logs/access-${domain}.log {`,
        '            roll_size 50MiB',
        '            roll_keep 3',
        '        }',
        '    }',
        '}'
    )
    return lines.join('\n')
}

// ============ Host Form Dialog ============
function HostFormDialog({ open, onClose, onSaved, host }) {
    const { t } = useTranslation()
    const [form, setForm] = useState({ ...DEFAULT_FORM })
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')
    const [certificates, setCertificates] = useState([])
    const [dnsProviders, setDnsProviders] = useState([])
    const [certFile, setCertFile] = useState(null)
    const [keyFile, setKeyFile] = useState(null)
    const [uploadingCert, setUploadingCert] = useState(false)
    const certFileRef = useReactRef(null)
    const keyFileRef = useReactRef(null)
    const isEdit = !!host
    const [fullCaddyAutoSync, setFullCaddyAutoSync] = useState(true)

    useEffect(() => {
        certificateAPI.list().then(res => setCertificates(res.data.certificates || [])).catch(() => { })
        dnsProviderAPI.list().then(res => setDnsProviders(res.data.providers || [])).catch(() => { })
    }, [])

    useEffect(() => {
        if (!dnsProviders.length || (form.tls_mode !== 'dns' && form.tls_mode !== 'wildcard')) return
        const hasSelectedProvider = dnsProviders.some((p) => p.id === form.dns_provider_id)
        if (hasSelectedProvider) return
        const defaultProvider = dnsProviders.find((p) => p.is_default) || dnsProviders[0]
        setForm((prev) => ({ ...prev, dns_provider_id: defaultProvider.id }))
    }, [dnsProviders, form.tls_mode, form.dns_provider_id])

    useEffect(() => {
        if (host) {
            const nextForm = {
                domain: hostDomainListFromForm({
                    domain: host.domain,
                    domains: host.domains,
                }).join(', '),
                host_type: host.host_type || 'proxy',
                tls_enabled: host.tls_enabled,
                http_redirect: host.http_redirect ?? false,
                websocket: host.websocket,
                enabled: host.enabled,
                upstreams: host.upstreams?.length
                    ? host.upstreams.map((u) => ({ address: u.address }))
                    : [{ address: '' }],
                redirect_url: host.redirect_url || '',
                redirect_code: host.redirect_code || 301,
                root_path: host.root_path || '',
                directory_browse: host.directory_browse || false,
                custom_headers: host.custom_headers || [],
                access_rules: host.access_rules || [],
                basic_auths: [], // never pre-fill passwords
                custom_directives: host.custom_directives || '',
                full_caddy_block: '',
                compression: host.compression || false,
                cors_enabled: host.cors_enabled || false,
                cors_origins: host.cors_origins || '*',
                cors_methods: host.cors_methods || 'GET, POST, PUT, DELETE, OPTIONS',
                cors_headers: host.cors_headers || 'Content-Type, Authorization',
                security_headers: host.security_headers || false,
                error_page_path: host.error_page_path || '',
                cache_enabled: host.cache_enabled || false,
                cache_ttl: host.cache_ttl || 300,
                tls_mode: host.tls_mode || 'auto',
                dns_provider_id: host.dns_provider_id || null,
            }
            nextForm.full_caddy_block = buildCaddyBlockFromForm(nextForm)
            setForm(nextForm)
            setFullCaddyAutoSync(true)
        } else {
            const nextForm = { ...DEFAULT_FORM }
            nextForm.full_caddy_block = buildCaddyBlockFromForm(nextForm)
            setForm(nextForm)
            setFullCaddyAutoSync(true)
        }
        setError('')
    }, [host, open])

    const handleUploadCert = async () => {
        setUploadingCert(true)
        try {
            const fd = new FormData()
            fd.append('name', form.domain || 'cert-' + Date.now())
            fd.append('cert', certFile)
            fd.append('key', keyFile)
            const res = await certificateAPI.upload(fd)
            const newCert = res.data.certificate
            setForm({ ...form, certificate_id: newCert.id })
            setCertFile(null)
            setKeyFile(null)
            certificateAPI.list().then(r => setCertificates(r.data.certificates || []))
        } catch (err) {
            setError(err.response?.data?.error || t('cert.upload_failed'))
        }
        setUploadingCert(false)
    }

    const handleSave = async () => {
        setError('')
        setSaving(true)
        try {
            const { domain, ...rest } = form
            const parsedDomains = parseAdditionalDomains(domain)
            const primaryDomain = parsedDomains[0] || domain.trim()
            const payload = {
                ...rest,
                full_caddy_block: fullCaddyAutoSync ? '' : rest.full_caddy_block,
                domain: primaryDomain,
                domains: parsedDomains.filter((item) => item.toLowerCase() !== primaryDomain.toLowerCase()),
                upstreams: form.host_type === 'proxy'
                    ? form.upstreams.filter((u) => u.address.trim())
                    : [],
                basic_auths: form.basic_auths.filter((a) => a.username && a.password),
            }
            if (isEdit) {
                await hostAPI.update(host.id, payload)
            } else {
                await hostAPI.create(payload)
            }
            onSaved()
            onClose()
        } catch (err) {
            setError(err.response?.data?.error || t('host.save_failed'))
        } finally {
            setSaving(false)
        }
    }

    const addUpstream = () => {
        setForm({ ...form, upstreams: [...form.upstreams, { address: '' }] })
    }

    const removeUpstream = (idx) => {
        const upstreams = form.upstreams.filter((_, i) => i !== idx)
        setForm({ ...form, upstreams: upstreams.length ? upstreams : [{ address: '' }] })
    }

    const updateUpstream = (idx, value) => {
        const upstreams = [...form.upstreams]
        upstreams[idx] = { address: value }
        setForm({ ...form, upstreams })
    }

    const updateDomain = (value) => {
        const nextPrimary = parseAdditionalDomains(value)[0] || value.trim()
        setForm((prev) => {
            const prevPrimary = parseAdditionalDomains(prev.domain)[0] || prev.domain.trim()
            const prevRoot = defaultSiteRootPath(prevPrimary)
            const prevError = defaultErrorPagePath(prevPrimary)
            return {
                ...prev,
                domain: value,
                root_path: !isEdit && (!prev.root_path || prev.root_path === prevRoot)
                    ? defaultSiteRootPath(nextPrimary)
                    : prev.root_path,
                error_page_path: !isEdit && (!prev.error_page_path || prev.error_page_path === prevError)
                    ? defaultErrorPagePath(nextPrimary)
                    : prev.error_page_path,
            }
        })
    }

    const buildDefaultCaddyBlock = useCallback(() => buildCaddyBlockFromForm(form), [form])

    useEffect(() => {
        if (!fullCaddyAutoSync) return
        setForm((prev) => {
            const nextBlock = buildCaddyBlockFromForm(prev)
            if (prev.full_caddy_block === nextBlock) return prev
            return { ...prev, full_caddy_block: nextBlock }
        })
    }, [
        fullCaddyAutoSync,
        form.domain,
        form.host_type,
        form.tls_mode,
        form.tls_enabled,
        form.http_redirect,
        form.websocket,
        form.compression,
        form.redirect_url,
        form.redirect_code,
        form.root_path,
        form.directory_browse,
        form.cors_enabled,
        form.cors_origins,
        form.cors_methods,
        form.cors_headers,
        form.security_headers,
        form.error_page_path,
        form.custom_directives,
        form.upstreams,
    ])

    const resetFullCaddyBlock = () => {
        setForm((prev) => {
            const nextForm = { ...prev }
            nextForm.full_caddy_block = buildCaddyBlockFromForm(nextForm)
            return nextForm
        })
        setFullCaddyAutoSync(true)
    }

    const addBasicAuth = () => {
        setForm({ ...form, basic_auths: [...form.basic_auths, { username: '', password: '' }] })
    }

    const removeBasicAuth = (idx) => {
        setForm({ ...form, basic_auths: form.basic_auths.filter((_, i) => i !== idx) })
    }

    const isProxy = form.host_type === 'proxy'
    const isStatic = form.host_type === 'static'
    const isRedirect = form.host_type === 'redirect'
    const needsRoot = isStatic
    const needsDnsProvider = form.tls_mode === 'dns' || form.tls_mode === 'wildcard'
    const parsedDomains = parseAdditionalDomains(form.domain)
    const primaryDomain = parsedDomains[0] || form.domain.trim()
    const canSave = parsedDomains.length > 0
        && (!isProxy || form.upstreams.some(u => u.address.trim()))
        && (!isRedirect || form.redirect_url.trim())
        && (!needsRoot || (form.root_path || '').trim())
        && (!needsDnsProvider || !!form.dns_provider_id)

    return (
        <Dialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <Dialog.Content maxWidth="560px" style={{ background: 'var(--cp-card)' }}>
                <Dialog.Title>
                    {isEdit ? t('host.edit_host') : t('host.add_host')}
                </Dialog.Title>
                <Dialog.Description size="2" color="gray" mb="4">
                    {isProxy ? t('host.proxy') : isRedirect ? t('host.redirect') : t('host.static')}
                </Dialog.Description>

                <Flex direction="column" gap="4">
                    {error && (
                        <Callout.Root color="red" size="1">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>{error}</Callout.Text>
                        </Callout.Root>
                    )}

                    {/* Host Type + Domain */}
                    <Flex gap="3" align="start">
                        <Flex direction="column" gap="1" style={{ width: 140 }}>
                            <Text size="2" weight="medium">{t('host.type')}</Text>
                            <Select.Root
                                value={form.host_type}
                                onValueChange={(v) => setForm({ ...form, host_type: v })}
                                size="2"
                            >
                                <Select.Trigger />
                                <Select.Content>
                                    <Select.Item value="proxy">{t('host.proxy')}</Select.Item>
                                    <Select.Item value="redirect">{t('host.redirect')}</Select.Item>
                                    <Select.Item value="static">{t('host.static')}</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Flex>
                        <Flex direction="column" gap="1" style={{ flex: 1 }}>
                            <Text size="2" weight="medium">{t('host.domain')}</Text>
                            <TextField.Root
                                placeholder="多个域名时使用逗号分隔填写"
                                value={form.domain}
                                onChange={(e) => updateDomain(e.target.value)}
                                size="2"
                            />
                        </Flex>
                    </Flex>

                    <Tabs.Root defaultValue="main">
                        <Tabs.List>
                            <Tabs.Trigger value="main">
                                {isProxy ? t('host.upstream') : isRedirect ? t('host.redirect') : t('host.options')}
                            </Tabs.Trigger>
                            <Tabs.Trigger value="options">{t('host.options')}</Tabs.Trigger>
                            <Tabs.Trigger value="auth">
                                <Lock size={12} style={{ marginRight: 4 }} />
                                {t('common.auth')}
                            </Tabs.Trigger>
                        </Tabs.List>

                        {/* Tab 1: Main config */}
                        <Tabs.Content value="main">
                            <Box pt="3">
                                {isProxy ? (
                                    /* Proxy: Upstreams */
                                    <Flex direction="column" gap="2">
                                        <Flex justify="between" align="center">
                                            <Text size="2" weight="medium">{t('host.upstreams')}</Text>
                                            <Button variant="ghost" size="1" onClick={addUpstream}>
                                                <Plus size={14} /> {t('common.add')}
                                            </Button>
                                        </Flex>
                                        {form.upstreams.map((u, i) => (
                                            <Flex key={i} gap="2" align="center">
                                                <TextField.Root
                                                    style={{ flex: 1 }}
                                                    placeholder="localhost:3000 or https://example.com"
                                                    value={u.address}
                                                    onChange={(e) => updateUpstream(i, e.target.value)}
                                                    size="2"
                                                />
                                                {form.upstreams.length > 1 && (
                                                    <IconButton
                                                        variant="ghost"
                                                        color="red"
                                                        size="1"
                                                        onClick={() => removeUpstream(i)}
                                                    >
                                                        <X size={14} />
                                                    </IconButton>
                                                )}
                                            </Flex>
                                        ))}
                                    </Flex>
                                ) : isRedirect ? (
                                    /* Redirect: Target URL + Code */
                                    <Flex direction="column" gap="3">
                                        <Flex direction="column" gap="1">
                                            <Text size="2" weight="medium">{t('host.redirect_url')}</Text>
                                            <TextField.Root
                                                placeholder="https://new-site.com"
                                                value={form.redirect_url}
                                                onChange={(e) => setForm({ ...form, redirect_url: e.target.value })}
                                                size="2"
                                            />
                                            <Text size="1" color="gray">
                                                {t('host.redirect_url_hint')}
                                            </Text>
                                        </Flex>
                                        <Flex direction="column" gap="1" style={{ width: 200 }}>
                                            <Text size="2" weight="medium">{t('host.redirect_code')}</Text>
                                            <Select.Root
                                                value={String(form.redirect_code)}
                                                onValueChange={(v) => setForm({ ...form, redirect_code: Number(v) })}
                                                size="2"
                                            >
                                                <Select.Trigger />
                                                <Select.Content>
                                                    <Select.Item value="301">{t('host.301_permanent')}</Select.Item>
                                                    <Select.Item value="302">{t('host.302_temporary')}</Select.Item>
                                                </Select.Content>
                                            </Select.Root>
                                        </Flex>
                                    </Flex>
                                ) : (
                                    /* Static: Root path + options */
                                    <Flex direction="column" gap="3">
                                        <Flex direction="column" gap="1">
                                            <Text size="2" weight="medium">{t('host.root_path')}</Text>
                                            <TextField.Root
                                                placeholder={defaultSiteRootPath(primaryDomain)}
                                                value={form.root_path || ''}
                                                onChange={(e) => setForm({ ...form, root_path: e.target.value })}
                                                size="2"
                                            />
                                            <Text size="1" color="gray">
                                                {t('host.static_root_hint')}
                                            </Text>
                                        </Flex>
                                        {isStatic && (
                                            <Flex justify="between" align="center">
                                                <Flex direction="column">
                                                    <Text size="2" weight="medium">{t('host.directory_browse')}</Text>
                                                    <Text size="1" color="gray">{t('host.directory_browse_hint')}</Text>
                                                </Flex>
                                                <Switch
                                                    checked={form.directory_browse || false}
                                                    onCheckedChange={(v) => setForm({ ...form, directory_browse: v })}
                                                />
                                            </Flex>
                                        )}
                                        <Flex direction="column" gap="1">
                                            <Text size="2" weight="medium">{t('host.index_files')}</Text>
                                            <TextField.Root
                                                placeholder="index.html index.htm"
                                                value={form.index_files || ''}
                                                onChange={(e) => setForm({ ...form, index_files: e.target.value })}
                                                size="2"
                                            />
                                            <Text size="1" color="gray">{t('host.index_files_hint')}</Text>
                                        </Flex>
                                    </Flex>
                                )}
                            </Box>
                        </Tabs.Content>

                        {/* Tab 2: Options */}
                        <Tabs.Content value="options">
                            <Card mt="3" style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                                <Flex direction="column" gap="3" p="1">
                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.tls_mode')}</Text>
                                            <Text size="1" color="gray">{t('host.tls_mode_hint')}</Text>
                                        </Flex>
                                        <Select.Root
                                            value={form.tls_mode || 'auto'}
                                            onValueChange={(v) => setForm({
                                                ...form,
                                                tls_mode: v,
                                                tls_enabled: v !== 'off',
                                                dns_provider_id: (v === 'dns' || v === 'wildcard') ? form.dns_provider_id : null,
                                            })}
                                            size="2"
                                        >
                                            <Select.Trigger style={{ width: 160 }} />
                                            <Select.Content>
                                                <Select.Item value="auto">{t('host.tls_auto')}</Select.Item>
                                                <Select.Item value="dns">{t('host.tls_dns')}</Select.Item>
                                                <Select.Item value="wildcard">{t('host.tls_wildcard')}</Select.Item>
                                                <Select.Item value="custom">{t('host.tls_custom')}</Select.Item>
                                                <Select.Item value="off">{t('host.tls_off')}</Select.Item>
                                            </Select.Content>
                                        </Select.Root>
                                    </Flex>

                                    {needsDnsProvider && (
                                        <Flex direction="column" gap="2" pl="4" style={{ borderLeft: '2px solid var(--cp-border-subtle)' }}>
                                            <Text size="1" color="gray">{t('dns.provider', 'DNS 提供商')}</Text>
                                            {dnsProviders.length > 0 ? (
                                                <Select.Root
                                                    value={form.dns_provider_id ? String(form.dns_provider_id) : undefined}
                                                    onValueChange={(v) => setForm({ ...form, dns_provider_id: Number(v) })}
                                                    size="2"
                                                >
                                                    <Select.Trigger placeholder={t('dns.provider', 'DNS 提供商')} />
                                                    <Select.Content>
                                                        {dnsProviders.map(p => (
                                                            <Select.Item key={p.id} value={String(p.id)}>
                                                                {p.name} — {p.provider}
                                                            </Select.Item>
                                                        ))}
                                                    </Select.Content>
                                                </Select.Root>
                                            ) : (
                                                <Text size="1" color="orange">
                                                    {t('dns.no_providers_hint', '尚未配置 DNS 提供商，请先在 DNS 提供商页面添加。')}
                                                </Text>
                                            )}
                                        </Flex>
                                    )}

                                    {form.tls_mode === 'custom' && (
                                        <Flex direction="column" gap="2" pl="4" style={{ borderLeft: '2px solid var(--cp-border-subtle)' }}>
                                            <Text size="1" color="gray">{t('cert.title')}</Text>
                                            <Select.Root
                                                value={form.certificate_id ? String(form.certificate_id) : ''}
                                                onValueChange={(v) => setForm({ ...form, certificate_id: v ? Number(v) : null })}
                                                size="2"
                                            >
                                                <Select.Trigger placeholder={t('host.select_cert_hint')} />
                                                <Select.Content>
                                                    {certificates.map(c => (
                                                        <Select.Item key={c.id} value={String(c.id)}>
                                                            {c.name} — {c.domains || t('common.unknown')}
                                                        </Select.Item>
                                                    ))}
                                                </Select.Content>
                                            </Select.Root>
                                            {certificates.length === 0 && (
                                                <Text size="1" color="orange">
                                                    {t('host.no_cert_hint')}
                                                </Text>
                                            )}

                                            <Separator size="4" />

                                            <Text size="1" color="gray">{t('host.upload_cert_hint')}</Text>
                                            <Flex gap="2">
                                                <Button
                                                    variant="soft" color="gray" size="1"
                                                    onClick={() => certFileRef.current?.click()}
                                                >
                                                    {certFile ? certFile.name : t('cert.choose_cert')}
                                                </Button>
                                                <Button
                                                    variant="soft" color="gray" size="1"
                                                    onClick={() => keyFileRef.current?.click()}
                                                >
                                                    {keyFile ? keyFile.name : t('cert.choose_key')}
                                                </Button>
                                                <input ref={certFileRef} type="file" accept=".pem,.crt,.cer" onChange={(e) => setCertFile(e.target.files?.[0])} style={{ display: 'none' }} />
                                                <input ref={keyFileRef} type="file" accept=".pem,.key" onChange={(e) => setKeyFile(e.target.files?.[0])} style={{ display: 'none' }} />
                                            </Flex>
                                            {certFile && keyFile && (
                                                <Button
                                                    size="1" variant="soft"
                                                    onClick={handleUploadCert}
                                                    disabled={uploadingCert}
                                                >
                                                    {uploadingCert ? t('common.loading') : t('host.upload_and_associate')}
                                                </Button>
                                            )}
                                        </Flex>
                                    )}

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.http_redirect')}</Text>
                                            <Text size="1" color="gray">{t('host.http_redirect_hint')}</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.http_redirect}
                                            onCheckedChange={(v) => setForm({ ...form, http_redirect: v })}
                                        />
                                    </Flex>

                                    {isProxy && (
                                        <Flex justify="between" align="center">
                                            <Flex direction="column">
                                                <Text size="2" weight="medium">{t('host.websocket')}</Text>
                                                <Text size="1" color="gray">{t('host.websocket_hint')}</Text>
                                            </Flex>
                                            <Switch
                                                checked={form.websocket}
                                                onCheckedChange={(v) => setForm({ ...form, websocket: v })}
                                            />
                                        </Flex>
                                    )}

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>{t('host.performance')}</Text>

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.compression')}</Text>
                                            <Text size="1" color="gray">{t('host.compression_hint')}</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.compression}
                                            onCheckedChange={(v) => setForm({ ...form, compression: v })}
                                        />
                                    </Flex>

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>{t('host.security')}</Text>

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.security_headers')}</Text>
                                            <Text size="1" color="gray">{t('host.security_headers_hint')}</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.security_headers}
                                            onCheckedChange={(v) => setForm({ ...form, security_headers: v })}
                                        />
                                    </Flex>

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.cors')}</Text>
                                            <Text size="1" color="gray">{t('host.cors_hint')}</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.cors_enabled}
                                            onCheckedChange={(v) => setForm({ ...form, cors_enabled: v })}
                                        />
                                    </Flex>

                                    {form.cors_enabled && (
                                        <Flex direction="column" gap="2" pl="4" style={{ borderLeft: '2px solid var(--cp-border-subtle)' }}>
                                            <Box>
                                                <Text size="1" color="gray" mb="1">{t('host.cors_origins')}</Text>
                                                <TextField.Root
                                                    value={form.cors_origins}
                                                    onChange={(e) => setForm({ ...form, cors_origins: e.target.value })}
                                                    placeholder="* or https://example.com"
                                                />
                                            </Box>
                                            <Box>
                                                <Text size="1" color="gray" mb="1">{t('host.cors_methods')}</Text>
                                                <TextField.Root
                                                    value={form.cors_methods}
                                                    onChange={(e) => setForm({ ...form, cors_methods: e.target.value })}
                                                    placeholder="GET, POST, PUT, DELETE, OPTIONS"
                                                />
                                            </Box>
                                            <Box>
                                                <Text size="1" color="gray" mb="1">{t('host.cors_headers')}</Text>
                                                <TextField.Root
                                                    value={form.cors_headers}
                                                    onChange={(e) => setForm({ ...form, cors_headers: e.target.value })}
                                                    placeholder="Content-Type, Authorization"
                                                />
                                            </Box>
                                        </Flex>
                                    )}

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>{t('host.error_page')}</Text>

                                    <Box>
                                        <Text size="2" weight="medium" mb="1">{t('host.error_page_path')}</Text>
                                        <Text size="1" color="gray" mb="2" as="p">
                                            {t('host.error_page_hint')}
                                        </Text>
                                        <TextField.Root
                                            value={form.error_page_path}
                                            onChange={(e) => setForm({ ...form, error_page_path: e.target.value })}
                                            placeholder={defaultErrorPagePath(primaryDomain)}
                                        />
                                    </Box>

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>{t('host.advanced')}</Text>

                                    <Box>
                                        <Flex justify="between" align="center" mb="1">
                                            <Text size="2" weight="medium">{t('host.full_caddy_block')}</Text>
                                            <Button type="button" variant="soft" size="1" onClick={resetFullCaddyBlock}>
                                                复位
                                            </Button>
                                        </Flex>
                                        <Text size="1" color="gray" mb="2" as="p">
                                            {t('host.full_caddy_block_hint')}
                                        </Text>
                                        <textarea
                                            value={form.full_caddy_block}
                                            onChange={(e) => {
                                                setFullCaddyAutoSync(false)
                                                setForm({ ...form, full_caddy_block: e.target.value })
                                            }}
                                            placeholder={buildDefaultCaddyBlock()}
                                            rows={10}
                                            className="custom-textarea"
                                            style={{ fontFamily: 'var(--font-mono, Consolas, monospace)' }}
                                        />
                                    </Box>
                                </Flex>
                            </Card>
                        </Tabs.Content>

                        {/* Tab 3: Basic Auth */}
                        <Tabs.Content value="auth">
                            <Box pt="3">
                                <Flex direction="column" gap="2">
                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.basic_auth')}</Text>
                                            <Text size="1" color="gray">{t('host.basic_auth_hint')}</Text>
                                        </Flex>
                                        <Button variant="ghost" size="1" onClick={addBasicAuth}>
                                            <Plus size={14} /> {t('host.add_auth_user')}
                                        </Button>
                                    </Flex>
                                    {form.basic_auths.length === 0 && (
                                        <Text size="2" color="gray" style={{ fontStyle: 'italic' }}>
                                            {t('host.no_auth_hint')}
                                        </Text>
                                    )}
                                    {form.basic_auths.map((auth, i) => (
                                        <Flex key={i} gap="2" align="center">
                                            <TextField.Root
                                                style={{ flex: 1 }}
                                                placeholder={t('common.username')}
                                                value={auth.username}
                                                onChange={(e) => {
                                                    const auths = [...form.basic_auths]
                                                    auths[i] = { ...auths[i], username: e.target.value }
                                                    setForm({ ...form, basic_auths: auths })
                                                }}
                                                size="2"
                                            />
                                            <TextField.Root
                                                style={{ flex: 1 }}
                                                placeholder={t('common.password')}
                                                type="password"
                                                value={auth.password}
                                                onChange={(e) => {
                                                    const auths = [...form.basic_auths]
                                                    auths[i] = { ...auths[i], password: e.target.value }
                                                    setForm({ ...form, basic_auths: auths })
                                                }}
                                                size="2"
                                            />
                                            <IconButton
                                                variant="ghost"
                                                color="red"
                                                size="1"
                                                onClick={() => removeBasicAuth(i)}
                                            >
                                                <X size={14} />
                                            </IconButton>
                                        </Flex>
                                    ))}
                                    {isEdit && host?.basic_auths?.length > 0 && form.basic_auths.length === 0 && (
                                        <Callout.Root size="1" color="blue">
                                            <Callout.Icon><Shield size={14} /></Callout.Icon>
                                            <Callout.Text>
                                                {t('host.existing_auth_hint', { count: host.basic_auths.length })}
                                            </Callout.Text>
                                        </Callout.Root>
                                    )}
                                </Flex>
                            </Box>
                        </Tabs.Content>
                    </Tabs.Root>
                </Flex>

                <Flex gap="3" mt="5" justify="end">
                    <Dialog.Close>
                        <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                    </Dialog.Close>
                    <Button
                        onClick={handleSave}
                        disabled={saving || !canSave}
                    >
                        {saving ? <Spinner size="1" /> : null}
                        {isEdit ? t('common.save') : t('common.create')}
                    </Button>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}

// ============ Clone Dialog ============
function CloneDialog({ open, onClose, host, onCloned, t }) {
    const [newDomain, setNewDomain] = useState('')
    const [cloning, setCloning] = useState(false)
    const [error, setError] = useState('')

    useEffect(() => {
        if (open) {
            setNewDomain('')
            setError('')
            setCloning(false)
        }
    }, [open])

    const handleClone = async () => {
        if (!newDomain.trim()) return
        setError('')
        setCloning(true)
        try {
            await hostAPI.clone(host.id, { domain: newDomain.trim() })
            onCloned()
            onClose()
        } catch (err) {
            setError(err.response?.data?.error || t('clone.failed'))
        } finally {
            setCloning(false)
        }
    }

    return (
        <Dialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <Dialog.Content maxWidth="420px" style={{ background: 'var(--cp-card)' }}>
                <Dialog.Title>{t('clone.title')}</Dialog.Title>
                <Flex direction="column" gap="4" mt="2">
                    {error && (
                        <Callout.Root color="red" size="1">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>{error}</Callout.Text>
                        </Callout.Root>
                    )}
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('clone.source_domain')}</Text>
                        <TextField.Root value={host?.domain || ''} disabled size="2" />
                    </Flex>
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('clone.new_domain')}</Text>
                        <TextField.Root
                            placeholder={t('clone.new_domain_placeholder')}
                            value={newDomain}
                            onChange={(e) => setNewDomain(e.target.value)}
                            size="2"
                            onKeyDown={(e) => e.key === 'Enter' && handleClone()}
                        />
                    </Flex>
                    <Flex gap="3" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button onClick={handleClone} disabled={cloning || !newDomain.trim()}>
                            {cloning ? <Spinner size="1" /> : <Copy size={14} />}
                            {cloning ? t('clone.cloning') : t('clone.submit')}
                        </Button>
                    </Flex>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}

// ============ Delete Confirmation ============
function DeleteDialog({ open, onClose, host, onConfirm }) {
    const { t } = useTranslation()
    const [deleting, setDeleting] = useState(false)
    const handleDelete = async () => {
        setDeleting(true)
        await onConfirm()
        setDeleting(false)
    }
    return (
        <AlertDialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <AlertDialog.Content maxWidth="400px" style={{ background: 'var(--cp-card)' }}>
                <AlertDialog.Title>{t('host.delete_title')}</AlertDialog.Title>
                <AlertDialog.Description size="2">
                    {t('host.confirm_delete', { domain: host?.domain })}
                </AlertDialog.Description>
                <Flex gap="3" mt="4" justify="end">
                    <AlertDialog.Cancel>
                        <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                    </AlertDialog.Cancel>
                    <AlertDialog.Action>
                        <Button color="red" onClick={handleDelete} disabled={deleting}>
                            {deleting ? <Spinner size="1" /> : <Trash2 size={14} />}
                            {t('common.delete')}
                        </Button>
                    </AlertDialog.Action>
                </Flex>
            </AlertDialog.Content>
        </AlertDialog.Root>
    )
}

// ============ Mobile Host Card ============
function HostCard({ host, t, onEdit, onDelete, onToggle, onClone, toggling }) {
    return (
        <Box className="mobile-host-card" mb="3">
            <Flex justify="between" align="start" mb="2">
                <Flex align="center" gap="2">
                    {host.host_type === 'redirect' ? (
                        <ArrowRightLeft size={14} color="#f59e0b" />
                    ) : host.host_type === 'static' ? (
                        <Globe size={14} color="#3b82f6" />
                    ) : (
                        <Globe size={14} color="#10b981" />
                    )}
                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text)', wordBreak: 'break-all' }}>
                        {host.domain}
                    </Text>
                </Flex>
                <Badge
                    color={host.host_type === 'redirect' ? 'orange' : host.host_type === 'static' ? 'blue' : 'green'}
                    variant="soft"
                    size="1"
                >
                    {host.host_type === 'redirect' ? t('host.redirect') : host.host_type === 'static' ? t('host.static') : t('host.proxy')}
                </Badge>
            </Flex>

            <Flex align="center" gap="2" mb="3">
                <Badge color={host.tls_enabled ? 'green' : 'gray'} variant="soft" size="1">
                    {host.tls_enabled ? 'HTTPS' : 'HTTP'}
                </Badge>
                <Badge color={host.enabled ? 'green' : 'gray'} variant="soft" size="1">
                    {host.enabled ? t('common.enabled') : t('common.disabled')}
                </Badge>
            </Flex>

            <Flex justify="between" align="center">
                <Tooltip content={host.enabled ? t('host.click_to_disable') : t('host.click_to_enable')}>
                    <Switch
                        checked={host.enabled}
                        onCheckedChange={() => onToggle(host)}
                        disabled={toggling === host.id}
                        size="1"
                    />
                </Tooltip>
                <Flex gap="2">
                    <Tooltip content={t('clone.tooltip')}>
                        <IconButton variant="soft" size="1" onClick={() => onClone(host)}>
                            <Copy size={14} />
                        </IconButton>
                    </Tooltip>
                    <Tooltip content={t('common.edit')}>
                        <IconButton variant="soft" size="1" onClick={() => onEdit(host)}>
                            <Pencil size={14} />
                        </IconButton>
                    </Tooltip>
                    <Tooltip content={t('common.delete')}>
                        <IconButton variant="soft" color="red" size="1" onClick={() => onDelete(host)}>
                            <Trash2 size={14} />
                        </IconButton>
                    </Tooltip>
                </Flex>
            </Flex>
        </Box>
    )
}

// ============ Host List Page ============
export default function HostList() {
    const { t } = useTranslation()
    const [hosts, setHosts] = useState([])
    const [loading, setLoading] = useState(true)
    const [editHost, setEditHost] = useState(null)
    const [showForm, setShowForm] = useState(false)
    const [deleteHost, setDeleteHost] = useState(null)
    const [cloneHost, setCloneHost] = useState(null)
    const [toggling, setToggling] = useState(null)
    const [isMobile, setIsMobile] = useState(() =>
        typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches
    )

    useEffect(() => {
        const mql = window.matchMedia('(max-width: 767px)')
        const handler = (e) => setIsMobile(e.matches)
        mql.addEventListener('change', handler)
        return () => mql.removeEventListener('change', handler)
    }, [])

    const fetchHosts = useCallback(async () => {
        try {
            const res = await hostAPI.list({})
            setHosts(res.data.hosts || [])
        } catch (err) {
            console.error('Failed to fetch hosts:', err)
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => {
        fetchHosts()
    }, [fetchHosts])

    const handleToggle = async (host) => {
        setToggling(host.id)
        try {
            await hostAPI.toggle(host.id)
            fetchHosts()
        } catch (err) {
            console.error('Failed to toggle host:', err)
        } finally {
            setToggling(null)
        }
    }

    const handleDelete = async () => {
        try {
            await hostAPI.delete(deleteHost.id)
            setDeleteHost(null)
            fetchHosts()
        } catch (err) {
            console.error('Failed to delete host:', err)
        }
    }

    const openCreate = () => {
        setEditHost(null)
        setShowForm(true)
    }

    const openEdit = (host) => {
        setEditHost(host)
        setShowForm(true)
    }

    const renderTargetCell = (host) => {
        if (host.host_type === 'redirect') {
            return (
                <Flex align="center" gap="1">
                    <ArrowRightLeft size={12} color="#f59e0b" />
                    <Text size="2" color="gray">{host.redirect_url}</Text>
                </Flex>
            )
        }
        if (host.host_type === 'static') {
            return (
                <Flex align="center" gap="1">
                    <Text size="1" color="gray">📂 {host.root_path || '-'}</Text>
                </Flex>
            )
        }
        return (
            <Flex direction="column" gap="1">
                {(host.upstreams || []).map((u, i) => (
                    <Flex key={i} align="center" gap="1">
                        <ChevronRight size={12} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="2" color="gray">{u.address}</Text>
                    </Flex>
                ))}
            </Flex>
        )
    }

    return (
        <Box>
            <Flex justify="between" align="center" mb="5">
                <Box>
                    <Heading size="6" style={{ color: 'var(--cp-text)' }}>{t('host.title')}</Heading>
                    <Text size="2" color="gray">
                        {t('host.subtitle')}
                    </Text>
                </Box>
                <Button size="2" onClick={openCreate}>
                    <Plus size={16} />
                    {t('host.add_host')}
                </Button>
            </Flex>

            {loading ? (
                <Flex justify="center" p="9">
                    <Spinner size="3" />
                </Flex>
            ) : hosts.length === 0 ? (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex direction="column" align="center" gap="3" p="6">
                        <Globe size={48} strokeWidth={1} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="3" color="gray">{t('common.no_data')}</Text>
                        <Button onClick={openCreate}>
                            <Plus size={16} /> {t('host.add_first_host')}
                        </Button>
                    </Flex>
                </Card>
            ) : isMobile ? (
                /* Mobile: Card list view */
                <Box>
                    {hosts.map((host) => (
                        <HostCard
                            key={host.id}
                            host={host}
                            t={t}
                            onEdit={openEdit}
                            onDelete={setDeleteHost}
                            onToggle={handleToggle}
                            onClone={setCloneHost}
                            toggling={toggling}
                        />
                    ))}
                </Box>
            ) : (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)', padding: 0 }}>
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('host.domain')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('host.target')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('host.tls')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell style={{ width: 140 }}>{t('common.actions')}</Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {hosts.map((host) => (
                                <Table.Row
                                    key={host.id}
                                    style={{ opacity: host.enabled ? 1 : 0.5 }}
                                >
                                    <Table.Cell>
                                        <Flex align="center" gap="2">
                                            {host.host_type === 'redirect' ? (
                                                <ArrowRightLeft size={14} color="#f59e0b" />
                                            ) : host.host_type === 'static' ? (
                                                <Globe size={14} color="#3b82f6" />
                                            ) : (
                                                <Globe size={14} color="#10b981" />
                                            )}
                                            <Text weight="medium">{host.domain}</Text>
                                            {host.domains?.length > 0 && (
                                                <Badge color="gray" variant="soft" size="1">
                                                    +{host.domains.length}
                                                </Badge>
                                            )}
                                            <Tooltip content={t('host.visit_site')}>
                                                <IconButton
                                                    size="1"
                                                    variant="ghost"
                                                    color="gray"
                                                    style={{ cursor: 'pointer' }}
                                                    onClick={(e) => {
                                                        e.stopPropagation()
                                                        window.open(`${host.tls_enabled ? 'https' : 'http'}://${host.domain}`, '_blank')
                                                    }}
                                                >
                                                    <ExternalLink size={12} />
                                                </IconButton>
                                            </Tooltip>
                                            {host.basic_auths?.length > 0 && (
                                                <Tooltip content={t('host.auth_protected_tooltip')}>
                                                    <Lock size={12} color="#8b5cf6" />
                                                </Tooltip>
                                            )}
                                        </Flex>
                                    </Table.Cell>
                                    <Table.Cell>
                                        {renderTargetCell(host)}
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Badge
                                            color={host.tls_enabled ? 'green' : 'gray'}
                                            variant="soft"
                                            size="1"
                                        >
                                            {host.tls_enabled ? 'HTTPS' : 'HTTP'}
                                        </Badge>
                                        {host.custom_cert_path && (
                                            <Badge color="blue" variant="soft" size="1" ml="1">
                                                {t('host.tls_custom')}
                                            </Badge>
                                        )}
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Tooltip content={host.enabled ? t('host.click_to_disable') : t('host.click_to_enable')}>
                                            <Switch
                                                checked={host.enabled}
                                                onCheckedChange={() => handleToggle(host)}
                                                disabled={toggling === host.id}
                                                size="1"
                                            />
                                        </Tooltip>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Flex gap="2">
                                            <Tooltip content={t('clone.tooltip')}>
                                                <IconButton
                                                    variant="ghost"
                                                    size="1"
                                                    onClick={() => setCloneHost(host)}
                                                >
                                                    <Copy size={14} />
                                                </IconButton>
                                            </Tooltip>
                                            <Tooltip content={t('common.edit')}>
                                                <IconButton
                                                    variant="ghost"
                                                    size="1"
                                                    onClick={() => openEdit(host)}
                                                >
                                                    <Pencil size={14} />
                                                </IconButton>
                                            </Tooltip>
                                            <Tooltip content={t('common.delete')}>
                                                <IconButton
                                                    variant="ghost"
                                                    color="red"
                                                    size="1"
                                                    onClick={() => setDeleteHost(host)}
                                                >
                                                    <Trash2 size={14} />
                                                </IconButton>
                                            </Tooltip>
                                        </Flex>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                </Card>
            )}

            {/* Form Dialog */}
            <HostFormDialog
                key={editHost ? `edit-${editHost.id}` : 'create'}
                open={showForm}
                onClose={() => {
                    setShowForm(false)
                    setEditHost(null)
                }}
                host={editHost}
                onSaved={fetchHosts}
            />

            {/* Delete Confirmation */}
            <DeleteDialog
                open={!!deleteHost}
                onClose={() => setDeleteHost(null)}
                host={deleteHost}
                onConfirm={handleDelete}
            />

            {/* Clone Dialog */}
            <CloneDialog
                open={!!cloneHost}
                onClose={() => setCloneHost(null)}
                host={cloneHost}
                onCloned={fetchHosts}
                t={t}
            />
        </Box>
    )
}
