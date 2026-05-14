import { useEffect, useState } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Callout, Badge,
    Tabs, TextField, IconButton, Dialog, Spinner,
    Select, Table, TextArea, Tooltip, AlertDialog, Switch,
} from '@radix-ui/themes'
import {
    AlertCircle, CheckCircle2, Download, FileText,
    Plus, RefreshCw, Shield, Trash2, Pencil, Star, Terminal,
    SlidersHorizontal,
} from 'lucide-react'
import {
    dnsProviderAPI,
    logAPI,
    settingAPI,
} from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router'
import CaddyfileEditor from './CaddyfileEditor.jsx'
import { applyPageTitle, formatPageTitle } from '../utils/pageTitle.js'

const VALID_TABS = ['basic', 'logs', 'dns', 'caddyfile', 'startup']

export default function Settings() {
    const { t } = useTranslation()
    const [searchParams] = useSearchParams()
    const initialTab = VALID_TABS.includes(searchParams.get('tab')) ? searchParams.get('tab') : 'basic'
    const [message, setMessage] = useState(null)

    const showMessage = (type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 5000)
    }

    return (
        <Box>
            <Heading size="6" mb="1" style={{ color: 'var(--cp-text)' }}>{t('settings.title')}</Heading>
            <Text size="2" color="gray" mb="5" as="p">
                {t('settings.subtitle')}
            </Text>

            {message && (
                <Callout.Root color={message.type === 'success' ? 'green' : 'red'} size="1" mb="4">
                    <Callout.Icon>
                        {message.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                    </Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Tabs.Root defaultValue={initialTab}>
                <Tabs.List style={{ flexWrap: 'wrap' }}>
                    <Tabs.Trigger value="basic">
                        <SlidersHorizontal size={14} style={{ marginRight: 6 }} /> {t('settings.tab_basic', '基础设置')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="logs">
                        <FileText size={14} style={{ marginRight: 6 }} /> {t('settings.tab_logs')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="dns">
                        <Shield size={14} style={{ marginRight: 6 }} /> {t('settings.tab_dns')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="caddyfile">
                        <FileText size={14} style={{ marginRight: 6 }} /> Caddy文件
                    </Tabs.Trigger>
                    <Tabs.Trigger value="startup">
                        <Terminal size={14} style={{ marginRight: 6 }} /> {t('settings.tab_startup', '启动脚本')}
                    </Tabs.Trigger>
                </Tabs.List>

                <Tabs.Content value="basic">
                    <BasicTab showMessage={showMessage} />
                </Tabs.Content>
                <Tabs.Content value="logs">
                    <LogsTab />
                </Tabs.Content>
                <Tabs.Content value="dns">
                    <DnsTab showMessage={showMessage} />
                </Tabs.Content>
                <Tabs.Content value="caddyfile">
                    <CaddyfileEditor embedded />
                </Tabs.Content>
                <Tabs.Content value="startup">
                    <StartupTab showMessage={showMessage} />
                </Tabs.Content>
            </Tabs.Root>
        </Box>
    )
}

function BasicTab({ showMessage }) {
    const { t } = useTranslation()
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')
    const [siteName, setSiteName] = useState('')

    const load = async () => {
        setLoading(true)
        setError('')
        try {
            const res = await settingAPI.getAll()
            setSiteName(res.data?.settings?.site_name || '')
        } catch (err) {
            setError(err.response?.data?.error || t('settings.load_failed', '加载设置失败'))
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => { load() }, [])

    const save = async () => {
        const value = siteName.trim()
        setSaving(true)
        setError('')
        try {
            await settingAPI.update('site_name', value)
            applyPageTitle(value)
            showMessage('success', t('settings.saved', '已保存'))
            setSiteName(value)
        } catch (err) {
            setError(err.response?.data?.error || t('settings.save_failed', '保存失败'))
        } finally {
            setSaving(false)
        }
    }

    return (
        <Box mt="4">
            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                <Flex justify="between" align="center" mb="4" wrap="wrap" gap="2">
                    <Box>
                        <Text size="3" weight="bold">{t('settings.tab_basic', '基础设置')}</Text>
                        <Text size="2" color="gray" as="p" mt="1">{t('settings.basic_hint', '设置面板的基础展示信息。')}</Text>
                    </Box>
                    <Button variant="soft" onClick={load} disabled={loading || saving}>
                        <RefreshCw size={14} /> {loading ? t('common.loading', '加载中') : t('common.refresh', '刷新')}
                    </Button>
                </Flex>

                {error && (
                    <Callout.Root color="red" size="1" mb="4">
                        <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                        <Callout.Text>{error}</Callout.Text>
                    </Callout.Root>
                )}

                {loading ? (
                    <Flex align="center" gap="2"><Spinner size="2" /> <Text color="gray">{t('common.loading', '加载中')}</Text></Flex>
                ) : (
                    <Flex direction="column" gap="4">
                        <Box>
                            <Text size="2" weight="medium">{t('settings.site_name_label', '站点名称')}</Text>
                            <Text size="1" color="gray" as="p" mt="1">
                                {t('settings.site_name_hint', '设置后页面标题将显示为“站点名称 - 派达[pdai.top]”。留空则使用默认标题。')}
                            </Text>
                            <TextField.Root
                                mt="2"
                                value={siteName}
                                onChange={(e) => setSiteName(e.target.value)}
                                maxLength={80}
                                placeholder={t('settings.site_name_placeholder', '例如：我的控制台')}
                            />
                        </Box>

                        <Box p="3" style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)', borderRadius: 8 }}>
                            <Text size="1" color="gray" as="p">{t('settings.page_title_preview', '页面标题预览')}</Text>
                            <Text size="2" weight="medium">{formatPageTitle(siteName)}</Text>
                        </Box>

                        <Flex justify="end" gap="2">
                            <Button variant="soft" onClick={load} disabled={saving}>{t('common.cancel', '取消')}</Button>
                            <Button onClick={save} disabled={saving}>{saving ? t('common.saving', '保存中') : t('common.save', '保存')}</Button>
                        </Flex>
                    </Flex>
                )}
            </Card>
        </Box>
    )
}

function LogsTab() {
    const { t } = useTranslation()
    const [type, setType] = useState('app')
    const [logs, setLogs] = useState('')
    const [loading, setLoading] = useState(false)

    const load = async () => {
        setLoading(true)
        try {
            const res = type === 'system' ? await logAPI.system({ lines: 300 }) : await logAPI.get({ type, lines: 300 })
            const lines = Array.isArray(res.data.lines) ? res.data.lines.join('\n') : ''
            setLogs(res.data.content || res.data.logs || lines || res.data.error || '')
        } catch (err) {
            setLogs(err.response?.data?.error || t('logs.load_failed', '日志加载失败'))
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => { load() }, [type])

    return (
        <Box mt="4">
            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                <Flex justify="between" align="center" mb="4" wrap="wrap" gap="3">
                    <Box>
                        <Text size="3" weight="bold">{t('settings.tab_logs')}</Text>
                        <Text size="2" color="gray" as="p" mt="1">{t('log.subtitle', '查看面板、Caddy 和系统日志')}</Text>
                    </Box>
                    <Flex gap="2" wrap="wrap">
                        <Select.Root value={type} onValueChange={setType}>
                            <Select.Trigger />
                            <Select.Content>
                                <Select.Item value="app">{t('logs.app', '应用日志')}</Select.Item>
                                <Select.Item value="caddy">Caddy</Select.Item>
                                <Select.Item value="system">{t('logs.system', '系统日志')}</Select.Item>
                            </Select.Content>
                        </Select.Root>
                        <Button variant="soft" onClick={load} disabled={loading}>
                            <RefreshCw size={14} /> {loading ? t('common.loading', '加载中') : t('common.refresh', '刷新')}
                        </Button>
                        <Button variant="soft" asChild>
                            <a href={logAPI.downloadUrl(type)}><Download size={14} /> {t('common.download', '下载')}</a>
                        </Button>
                    </Flex>
                </Flex>
                <TextArea
                    value={logs}
                    readOnly
                    rows={22}
                    placeholder={loading ? t('common.loading', '加载中') : t('log.no_logs', '未找到日志记录')}
                    style={{
                        fontFamily: 'var(--font-mono, Consolas, monospace)',
                        fontSize: 12,
                        lineHeight: 1.6,
                        background: 'var(--cp-input-bg)',
                        border: '1px solid var(--cp-border-subtle)',
                    }}
                />
            </Card>
        </Box>
    )
}

function StartupTab({ showMessage }) {
    const { t } = useTranslation()
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')
    const [form, setForm] = useState({
        panel_autostart: 'false',
        startup_script: '',
    })

    const load = async () => {
        setLoading(true)
        setError('')
        try {
            const res = await settingAPI.getAll()
            const settings = res.data.settings || {}
            setForm({
                panel_autostart: settings.panel_autostart ?? 'false',
                startup_script: settings.startup_script ?? '',
            })
        } catch (err) {
            setError(err.response?.data?.error || t('settings.load_failed', '加载设置失败'))
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => { load() }, [])

    const save = async () => {
        setSaving(true)
        setError('')
        try {
            await settingAPI.update('panel_autostart', form.panel_autostart === 'true' ? 'true' : 'false')
            await settingAPI.update('startup_script', form.startup_script || '')
            showMessage('success', t('settings.saved', '已保存'))
            await load()
        } catch (err) {
            setError(err.response?.data?.error || t('settings.save_failed', '保存失败'))
        } finally {
            setSaving(false)
        }
    }

    return (
        <Box mt="4">
            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                <Flex justify="between" align="center" mb="4" wrap="wrap" gap="2">
                    <Box>
                        <Text size="3" weight="bold">{t('settings.tab_startup', '启动脚本')}</Text>
                        <Text size="2" color="gray" as="p" mt="1">{t('settings.startup_hint', '控制面板是否开机自启，并编辑启动时执行的自定义脚本。')}</Text>
                    </Box>
                    <Button variant="soft" onClick={load} disabled={loading || saving}>
                        <RefreshCw size={14} /> {loading ? t('common.loading', '加载中') : t('common.refresh', '刷新')}
                    </Button>
                </Flex>

                {error && (
                    <Callout.Root color="red" size="1" mb="4">
                        <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                        <Callout.Text>{error}</Callout.Text>
                    </Callout.Root>
                )}

                {loading ? (
                    <Flex direction="column" gap="3">
                        <Box style={{ height: 84, borderRadius: 14, background: 'linear-gradient(90deg, var(--cp-input-bg) 0%, rgba(255,255,255,0.7) 50%, var(--cp-input-bg) 100%)', backgroundSize: '200% 100%', animation: 'pulse 1.2s ease-in-out infinite' }} />
                        <Box style={{ height: 220, borderRadius: 14, background: 'linear-gradient(90deg, var(--cp-input-bg) 0%, rgba(255,255,255,0.7) 50%, var(--cp-input-bg) 100%)', backgroundSize: '200% 100%', animation: 'pulse 1.2s ease-in-out infinite' }} />
                    </Flex>
                ) : (
                    <Flex direction="column" gap="4">
                        <Flex align="center" justify="between" p="4" style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)', borderRadius: 16 }}>
                            <Box>
                                <Text size="2" weight="medium">{t('settings.panel_autostart_label', '开机自动启动面板')}</Text>
                                <Text size="1" color="gray" as="p">{t('settings.panel_autostart_hint', '启用后，安装时生成的 systemd 服务会随系统启动自动拉起面板。')}</Text>
                            </Box>
                            <Switch checked={form.panel_autostart === 'true'} onCheckedChange={(value) => setForm((prev) => ({ ...prev, panel_autostart: value ? 'true' : 'false' }))} />
                        </Flex>

                        <Box>
                            <Text size="2" weight="medium">{t('settings.startup_script_label', '自定义启动脚本')}</Text>
                            <Text size="1" color="gray" as="p" mt="1">{t('settings.startup_script_hint', '面板启动后会执行这里填写的脚本。请只填写需要额外运行的命令，按行分隔。')}</Text>
                            <TextArea
                                mt="2"
                                rows={12}
                                value={form.startup_script}
                                onChange={(e) => setForm((prev) => ({ ...prev, startup_script: e.target.value }))}
                                placeholder={t('settings.startup_script_placeholder', '例如：\nexport NODE_ENV=production\n./scripts/start-extra.sh')}
                                style={{
                                    fontFamily: 'var(--font-mono, Consolas, monospace)',
                                    fontSize: 13,
                                    lineHeight: 1.6,
                                    background: 'var(--cp-input-bg)',
                                    border: '1px solid var(--cp-border-subtle)',
                                }}
                            />
                        </Box>

                        <Flex justify="end" gap="2">
                            <Button variant="soft" onClick={load} disabled={saving}>{t('common.cancel', '取消')}</Button>
                            <Button onClick={save} disabled={saving}>{saving ? t('common.saving', '保存中') : t('common.save', '保存')}</Button>
                        </Flex>
                    </Flex>
                )}
            </Card>
        </Box>
    )
}

function DnsTab({ showMessage }) {
    const { t } = useTranslation()
    const providerFields = {
        cloudflare: { label: t('dns.cloudflare', 'Cloudflare'), fields: [{ key: 'api_token', label: t('dns.api_token', 'API Token'), placeholder: 'Cloudflare API Token' }] },
        alidns: { label: t('dns.alidns', '阿里云 DNS'), fields: [{ key: 'access_key_id', label: t('dns.access_key_id', 'Access Key ID'), placeholder: 'LTAI...' }, { key: 'access_key_secret', label: t('dns.access_key_secret', 'Access Key Secret'), placeholder: 'AccessKeySecret' }] },
        tencentcloud: { label: t('dns.tencentcloud', '腾讯云 DNS'), fields: [{ key: 'secret_id', label: t('dns.secret_id', 'Secret ID'), placeholder: 'AKIDxxxxxxxx' }, { key: 'secret_key', label: t('dns.secret_key', 'Secret Key'), placeholder: 'SecretKey' }] },
        route53: { label: t('dns.route53', 'AWS Route 53'), fields: [{ key: 'region', label: t('dns.region', 'Region'), placeholder: 'us-east-1' }, { key: 'access_key_id', label: t('dns.access_key_id', 'Access Key ID'), placeholder: 'AKIA...' }, { key: 'secret_access_key', label: t('dns.access_key_secret', 'Secret Access Key'), placeholder: 'SecretAccessKey' }] },
    }
    const defaultForm = { name: '', provider: 'cloudflare', config: {}, is_default: false }
    const [providers, setProviders] = useState([])
    const [loading, setLoading] = useState(true)
    const [open, setOpen] = useState(false)
    const [editing, setEditing] = useState(null)
    const [form, setForm] = useState(defaultForm)
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')
    const [deleteTarget, setDeleteTarget] = useState(null)

    const normalizeConfig = (value) => {
        if (!value) return {}
        if (typeof value === 'string') {
            try { return JSON.parse(value) } catch { return {} }
        }
        return value
    }

    const load = async () => {
        setLoading(true)
        try {
            const res = await dnsProviderAPI.list()
            setProviders(res.data.providers || [])
        } catch {
            showMessage('error', t('dns.load_failed', '无法加载 DNS 提供商'))
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => { load() }, [])

    const openCreate = () => {
        setEditing(null)
        setError('')
        setForm(defaultForm)
        setOpen(true)
    }

    const openEdit = async (provider) => {
        setEditing(provider)
        setError('')
        try {
            const res = await dnsProviderAPI.get(provider.id)
            const data = res.data || provider
            setForm({
                name: data.name || '',
                provider: data.provider || 'cloudflare',
                config: normalizeConfig(data.config),
                is_default: !!data.is_default,
            })
        } catch {
            setForm({
                name: provider.name || '',
                provider: provider.provider || provider.type || 'cloudflare',
                config: normalizeConfig(provider.config),
                is_default: !!provider.is_default,
            })
        }
        setOpen(true)
    }

    const setConfigField = (key, value) => {
        setForm((prev) => ({ ...prev, config: { ...prev.config, [key]: value } }))
    }

    const save = async () => {
        setError('')
        setSaving(true)
        try {
            const payload = {
                name: form.name,
                provider: form.provider,
                config: JSON.stringify(form.config || {}),
                is_default: form.is_default,
            }
            if (editing) await dnsProviderAPI.update(editing.id, payload)
            else await dnsProviderAPI.create(payload)
            setOpen(false)
            setEditing(null)
            setForm(defaultForm)
            showMessage('success', t('settings.saved', '已保存'))
            await load()
        } catch (err) {
            setError(err.response?.data?.error || t('settings.save_failed', '保存失败'))
        } finally {
            setSaving(false)
        }
    }

    const remove = async () => {
        if (!deleteTarget) return
        try {
            await dnsProviderAPI.delete(deleteTarget.id)
            setDeleteTarget(null)
            showMessage('success', t('common.deleted', '删除成功'))
            await load()
        } catch (err) {
            setDeleteTarget(null)
            showMessage('error', err.response?.data?.error || t('common.delete_failed', '删除失败'))
        }
    }

    const providerDef = providerFields[form.provider] || providerFields.cloudflare

    return (
        <Box mt="4">
            <Flex justify="between" align="center" mb="4" wrap="wrap" gap="2">
                <Box>
                    <Text size="3" weight="bold">{t('dns.title', 'DNS 提供商')}</Text>
                    <Text size="2" color="gray" as="p" mt="1">{t('dns.subtitle', '管理 DNS API 提供商，用于 ACME DNS Challenge 申请证书')}</Text>
                </Box>
                <Button size="2" onClick={openCreate}><Plus size={14} /> {t('dns.add_provider', '添加提供商')}</Button>
            </Flex>

            {loading ? (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2"><Spinner size="2" /> <Text color="gray">{t('common.loading', '加载中')}</Text></Flex>
                </Card>
            ) : providers.length === 0 ? (
                <Card style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                    <Flex direction="column" align="center" gap="3" py="6">
                        <Shield size={40} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text color="gray">{t('dns.no_providers', '还没有配置 DNS 提供商')}</Text>
                        <Button variant="soft" size="2" onClick={openCreate}><Plus size={14} /> {t('dns.add_first', '添加第一个')}</Button>
                    </Flex>
                </Card>
            ) : (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('common.name', '名称')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('dns.provider', '提供商')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('dns.is_default', '默认')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell style={{ width: 120 }}>{t('common.actions', '操作')}</Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {providers.map((p) => (
                                <Table.Row key={p.id}>
                                    <Table.Cell><Flex align="center" gap="2"><Shield size={14} color="#10b981" /><Text weight="medium">{p.name}</Text></Flex></Table.Cell>
                                    <Table.Cell><Badge variant="soft" size="1">{providerFields[p.provider]?.label || p.provider || p.type}</Badge></Table.Cell>
                                    <Table.Cell>{p.is_default && <Tooltip content={t('dns.default_provider_tooltip', '默认提供商')}><Star size={14} color="#f59e0b" fill="#f59e0b" /></Tooltip>}</Table.Cell>
                                    <Table.Cell>
                                        <Flex gap="2">
                                            <Tooltip content={t('common.edit', '编辑')}><IconButton variant="ghost" size="1" onClick={() => openEdit(p)}><Pencil size={14} /></IconButton></Tooltip>
                                            <Tooltip content={t('common.delete', '删除')}><IconButton variant="ghost" size="1" color="red" onClick={() => setDeleteTarget(p)}><Trash2 size={14} /></IconButton></Tooltip>
                                        </Flex>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                </Card>
            )}

            <Dialog.Root open={open} onOpenChange={setOpen}>
                <Dialog.Content maxWidth="480px" style={{ background: 'var(--cp-card)' }}>
                    <Dialog.Title>{editing ? t('dns.edit_provider', '编辑 DNS 提供商') : t('dns.add_provider', '添加提供商')}</Dialog.Title>
                    <Dialog.Description size="2" color="gray" mb="4">{t('dns.dialog_description', '配置 DNS API 凭据用于证书 DNS 验证')}</Dialog.Description>
                    <Flex direction="column" gap="3">
                        {error && <Callout.Root color="red" size="1"><Callout.Icon><AlertCircle size={14} /></Callout.Icon><Callout.Text>{error}</Callout.Text></Callout.Root>}
                        <Box>
                            <Text size="2" weight="medium">{t('dns.name', '名称')}</Text>
                            <TextField.Root mt="1" placeholder={t('dns.name_placeholder', '例如：我的 Cloudflare')} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
                        </Box>
                        <Box>
                            <Flex direction="column" gap="1">
                                <Text size="2" weight="medium">{t('dns.provider_type', '提供商类型')}</Text>
                                <Select.Root value={form.provider} onValueChange={(value) => setForm({ ...form, provider: value, config: {} })}>
                                    <Select.Trigger />
                                    <Select.Content>
                                        {Object.entries(providerFields).map(([value, def]) => <Select.Item key={value} value={value}>{def.label}</Select.Item>)}
                                    </Select.Content>
                                </Select.Root>
                            </Flex>
                        </Box>
                        {providerDef.fields.map((field) => (
                            <Box key={field.key}>
                                <Text size="2" weight="medium">{field.label}</Text>
                                <TextField.Root mt="1" placeholder={field.placeholder} type={field.key.includes('secret') || field.key.includes('token') || field.key.includes('key') ? 'password' : 'text'} value={form.config?.[field.key] || ''} onChange={(e) => setConfigField(field.key, e.target.value)} />
                            </Box>
                        ))}
                        <Flex align="center" justify="between" p="3" style={{ background: 'var(--cp-input-bg)', borderRadius: 8 }}>
                            <Box>
                                <Text size="2" weight="medium">{t('dns.set_default', '设为默认')}</Text>
                                <Text size="1" color="gray" as="p">{t('dns.set_default_hint', '新建站点自动使用此提供商')}</Text>
                            </Box>
                            <Switch checked={form.is_default} onCheckedChange={(value) => setForm({ ...form, is_default: value })} />
                        </Flex>
                        <Flex justify="end" gap="2" mt="2">
                            <Button variant="soft" onClick={() => setOpen(false)}>{t('common.cancel', '取消')}</Button>
                            <Button onClick={save} disabled={saving || !form.name.trim()}>{saving ? t('common.saving', '保存中') : t('common.save', '保存')}</Button>
                        </Flex>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            <AlertDialog.Root open={!!deleteTarget} onOpenChange={(openDialog) => !openDialog && setDeleteTarget(null)}>
                <AlertDialog.Content maxWidth="420px">
                    <AlertDialog.Title>{t('dns.confirm_delete_title', '确认删除')}</AlertDialog.Title>
                    <AlertDialog.Description>{t('dns.confirm_delete_desc', '删除后使用此提供商的站点将无法续签证书，确定要继续吗？')}</AlertDialog.Description>
                    <Flex justify="end" gap="2" mt="4">
                        <AlertDialog.Cancel><Button variant="soft">{t('common.cancel', '取消')}</Button></AlertDialog.Cancel>
                        <AlertDialog.Action><Button color="red" onClick={remove}>{t('dns.confirm_delete_btn', '确认删除')}</Button></AlertDialog.Action>
                    </Flex>
                </AlertDialog.Content>
            </AlertDialog.Root>
        </Box>
    )
}
