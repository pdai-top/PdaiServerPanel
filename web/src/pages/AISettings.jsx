import { useEffect, useState } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Callout, TextField,
    Select, Spinner,
} from '@radix-ui/themes'
import { AlertCircle, CheckCircle2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { aiAPI } from '../api/index.js'

const defaultForm = {
    base_url: '',
    api_key: '',
    model: '',
    api_format: 'openai-chat',
}

export default function AISettings() {
    const { t } = useTranslation()
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [testing, setTesting] = useState(false)
    const [message, setMessage] = useState(null)
    const [error, setError] = useState('')
    const [form, setForm] = useState(defaultForm)
    const [compact, setCompact] = useState(() =>
        typeof window !== 'undefined' && window.matchMedia('(max-width: 720px)').matches
    )

    const showMessage = (type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 5000)
    }

    const load = async () => {
        setLoading(true)
        setError('')
        try {
            const res = await aiAPI.getConfig()
            const cfg = res.data || {}
            setForm({
                base_url: cfg.base_url || '',
                api_key: cfg.api_key || '',
                model: cfg.model || '',
                api_format: cfg.api_format || 'openai-chat',
            })
        } catch (err) {
            setError(err.response?.data?.error || t('settings.load_failed', 'Load failed'))
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => { load() }, [])

    useEffect(() => {
        const mql = window.matchMedia('(max-width: 720px)')
        const handler = (e) => setCompact(e.matches)
        mql.addEventListener('change', handler)
        return () => mql.removeEventListener('change', handler)
    }, [])

    const save = async () => {
        setSaving(true)
        setError('')
        try {
            await aiAPI.updateConfig(form)
            showMessage('success', t('ai.config_saved', 'AI configuration saved'))
            await load()
        } catch (err) {
            setError(err.response?.data?.error || t('settings.save_failed', 'Save failed'))
        } finally {
            setSaving(false)
        }
    }

    const test = async () => {
        setTesting(true)
        setError('')
        try {
            await aiAPI.updateConfig(form)
            await aiAPI.testConnection()
            showMessage('success', t('ai.connection_ok', 'Connection successful! AI is ready to use.'))
            await load()
        } catch (err) {
            setError(err.response?.data?.error || t('common.operation_failed', 'Operation failed'))
        } finally {
            setTesting(false)
        }
    }

    const busy = saving || testing

    return (
        <Box>
            <Heading size="6" mb="1" style={{ color: 'var(--cp-text)' }}>
                {t('ai.settings_title', 'AI Settings')}
            </Heading>
            <Text size="2" color="gray" mb="5" as="p">
                {t('plugins.descriptions.ai', 'AI-powered chat assistant with error diagnosis and template generation')}
            </Text>

            {message && (
                <Callout.Root color={message.type === 'success' ? 'green' : 'red'} size="1" mb="4">
                    <Callout.Icon>
                        {message.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                    </Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                <Flex justify="between" align="center" mb="4" wrap="wrap" gap="2">
                    <Box>
                        <Text size="3" weight="bold">{t('ai.tab_chat_model', 'Chat Model')}</Text>
                        <Text size="2" color="gray" as="p" mt="1">
                            {t('ai.api_key_hint', 'Your API key will be encrypted and stored securely')}
                        </Text>
                    </Box>
                    <Button variant="soft" onClick={load} disabled={loading || busy}>
                        <RefreshCw size={14} /> {loading ? t('common.loading', 'Loading...') : t('common.refresh', 'Refresh')}
                    </Button>
                </Flex>

                {error && (
                    <Callout.Root color="red" size="1" mb="4">
                        <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                        <Callout.Text>{error}</Callout.Text>
                    </Callout.Root>
                )}

                {loading ? (
                    <Flex align="center" gap="2">
                        <Spinner size="2" />
                        <Text color="gray">{t('common.loading', 'Loading...')}</Text>
                    </Flex>
                ) : (
                    <Flex direction="column" gap="4">
                        <Box style={{ display: 'grid', gridTemplateColumns: compact ? '1fr' : '280px minmax(0, 1fr)', gap: '12px', alignItems: 'end' }}>
                            <Box style={{ minWidth: 0 }}>
                                <Text size="2" weight="medium">{t('ai.api_format', 'API Format')}</Text>
                                <Select.Root value={form.api_format} onValueChange={(value) => setForm({ ...form, api_format: value })}>
                                    <Select.Trigger mt="2" style={{ width: '100%' }} />
                                    <Select.Content>
                                        <Select.Item value="openai-chat">OpenAI Chat Completions</Select.Item>
                                        <Select.Item value="anthropic-messages">Anthropic Messages</Select.Item>
                                        <Select.Item value="google-generativeai">Google Generative AI</Select.Item>
                                    </Select.Content>
                                </Select.Root>
                            </Box>
                            <Box style={{ minWidth: 0 }}>
                                <Text size="2" weight="medium">{t('ai.base_url', 'API Base URL')}</Text>
                                <TextField.Root mt="2" style={{ width: '100%' }} value={form.base_url} onChange={(e) => setForm({ ...form, base_url: e.target.value })} placeholder="https://api.openai.com" />
                            </Box>
                        </Box>

                        <Flex gap="3" wrap="wrap">
                            <Box style={{ flex: '1 1 260px' }}>
                                <Text size="2" weight="medium">{t('ai.model', 'Model')}</Text>
                                <TextField.Root mt="2" value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })} placeholder="gpt-4o-mini" />
                            </Box>
                            <Box style={{ flex: '1 1 260px' }}>
                                <Text size="2" weight="medium">{t('ai.api_key', 'API Key')}</Text>
                                <TextField.Root mt="2" type="password" value={form.api_key} onChange={(e) => setForm({ ...form, api_key: e.target.value })} placeholder="sk-..." />
                            </Box>
                        </Flex>

                        <Flex justify="end" gap="2" wrap="wrap">
                            <Button variant="soft" onClick={test} disabled={busy || !form.base_url.trim() || !form.model.trim()}>
                                {testing && <Spinner size="1" />} {t('ai.test_connection', 'Test Connection')}
                            </Button>
                            <Button onClick={save} disabled={busy || !form.base_url.trim() || !form.model.trim()}>
                                {saving && <Spinner size="1" />} {t('common.save', 'Save')}
                            </Button>
                        </Flex>
                    </Flex>
                )}
            </Card>
        </Box>
    )
}
