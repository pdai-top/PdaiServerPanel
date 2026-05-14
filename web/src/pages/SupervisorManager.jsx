import { useState, useEffect, useCallback } from 'react'
import {
    Box, Flex, Text, Card, Badge, Button, Table, Dialog, TextField,
    Switch, TextArea, Heading, Callout, ScrollArea, Tabs,
} from '@radix-ui/themes'
import {
    ServerCog, Play, Square, RotateCw, Plus, Pencil, Trash2, RefreshCw,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { supervisorAPI } from '../api/index.js'

const defaultForm = {
    name: '',
    command: '',
    working_dir: '',
    env: '',
    enabled: true,
    autostart: true,
    autorestart: true,
    restart_delay_sec: 3,
    stop_timeout_sec: 10,
    max_restarts: 10,
}

function formatDate(ts) {
    if (!ts) return '-'
    const d = new Date(ts)
    if (Number.isNaN(d.getTime())) return '-'
    return d.toLocaleString()
}

function statusBadge(status, t) {
    const map = {
        running: { color: 'green', label: t('supervisor.status_running') },
        stopped: { color: 'gray', label: t('supervisor.status_stopped') },
        starting: { color: 'blue', label: t('supervisor.status_starting') },
        stopping: { color: 'orange', label: t('supervisor.status_stopping') },
        failed: { color: 'red', label: t('supervisor.status_failed') },
    }
    const item = map[status] || { color: 'gray', label: status || '-' }
    return <Badge color={item.color} variant="soft">{item.label}</Badge>
}

function streamBadge(stream) {
    const color = stream === 'stderr' ? 'red' : stream === 'system' ? 'blue' : 'gray'
    return <Badge color={color} variant="soft">{stream || '-'}</Badge>
}

export default function SupervisorManager() {
    const { t } = useTranslation()
    const [processes, setProcesses] = useState([])
    const [logs, setLogs] = useState([])
    const [loading, setLoading] = useState(true)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [editId, setEditId] = useState(null)
    const [form, setForm] = useState({ ...defaultForm })
    const [saving, setSaving] = useState(false)
    const [activeTab, setActiveTab] = useState('processes')
    const [logProcessId, setLogProcessId] = useState(null)
    const [deleteConfirm, setDeleteConfirm] = useState(null)

    const fetchProcesses = useCallback(async () => {
        try {
            const res = await supervisorAPI.listProcesses()
            setProcesses(res.data || [])
        } catch { /* ignore */ }
    }, [])

    const fetchLogs = useCallback(async (processId) => {
        try {
            const res = processId
                ? await supervisorAPI.processLogs(processId, 100)
                : await supervisorAPI.allLogs(100)
            setLogs(res.data || [])
        } catch { /* ignore */ }
    }, [])

    const fetchAll = useCallback(async () => {
        setLoading(true)
        await fetchProcesses()
        await fetchLogs(logProcessId)
        setLoading(false)
    }, [fetchProcesses, fetchLogs, logProcessId])

    useEffect(() => { fetchAll() }, [fetchAll])

    const openCreate = () => {
        setEditId(null)
        setForm({ ...defaultForm })
        setDialogOpen(true)
    }

    const openEdit = (item) => {
        setEditId(item.id)
        setForm({
            name: item.name || '',
            command: item.command || '',
            working_dir: item.working_dir || '',
            env: item.env || '',
            enabled: item.enabled !== false,
            autostart: item.autostart !== false,
            autorestart: item.autorestart !== false,
            restart_delay_sec: item.restart_delay_sec || 3,
            stop_timeout_sec: item.stop_timeout_sec || 10,
            max_restarts: item.max_restarts || 10,
        })
        setDialogOpen(true)
    }

    const handleSave = async () => {
        setSaving(true)
        try {
            const data = {
                ...form,
                restart_delay_sec: Number(form.restart_delay_sec) || 3,
                stop_timeout_sec: Number(form.stop_timeout_sec) || 10,
                max_restarts: Number(form.max_restarts) || 10,
            }
            if (editId) await supervisorAPI.updateProcess(editId, data)
            else await supervisorAPI.createProcess(data)
            setDialogOpen(false)
            fetchAll()
        } catch (e) {
            alert(e?.response?.data?.error || e.message)
        } finally {
            setSaving(false)
        }
    }

    const runAction = async (action, id) => {
        try {
            if (action === 'start') await supervisorAPI.startProcess(id)
            if (action === 'stop') await supervisorAPI.stopProcess(id)
            if (action === 'restart') await supervisorAPI.restartProcess(id)
            setTimeout(fetchAll, 500)
        } catch (e) {
            alert(e?.response?.data?.error || e.message)
        }
    }

    const handleDelete = async () => {
        if (!deleteConfirm) return
        try {
            await supervisorAPI.deleteProcess(deleteConfirm.id)
            setDeleteConfirm(null)
            fetchAll()
        } catch (e) {
            alert(e?.response?.data?.error || e.message)
        }
    }

    const showLogs = (item) => {
        setLogProcessId(item?.id || null)
        setActiveTab('logs')
        fetchLogs(item?.id || null)
    }

    return (
        <Box p="5">
            <Flex justify="between" align="center" mb="5" gap="3" wrap="wrap">
                <Box>
                    <Heading size="7"><ServerCog size={28} style={{ verticalAlign: 'middle', marginRight: 10 }} />{t('supervisor.title')}</Heading>
                    <Text color="gray" mt="2" as="p">{t('supervisor.subtitle')}</Text>
                </Box>
                <Flex gap="2">
                    <Button variant="soft" onClick={fetchAll}><RefreshCw size={16} />{t('common.refresh')}</Button>
                    <Button onClick={openCreate}><Plus size={16} />{t('supervisor.new_process')}</Button>
                </Flex>
            </Flex>

            <Tabs.Root value={activeTab} onValueChange={setActiveTab}>
                <Tabs.List>
                    <Tabs.Trigger value="processes">{t('supervisor.processes')}</Tabs.Trigger>
                    <Tabs.Trigger value="logs">{t('supervisor.logs')}</Tabs.Trigger>
                </Tabs.List>

                <Box pt="4">
                    <Tabs.Content value="processes">
                        <Card>
                            {loading ? (
                                <Text color="gray">{t('common.loading')}</Text>
                            ) : processes.length === 0 ? (
                                <Callout.Root color="gray"><Callout.Text>{t('supervisor.no_processes')}</Callout.Text></Callout.Root>
                            ) : (
                                <Table.Root variant="surface">
                                    <Table.Header>
                                        <Table.Row>
                                            <Table.ColumnHeaderCell>{t('supervisor.process_name')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('supervisor.status')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>PID</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('supervisor.command')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('supervisor.autostart')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('supervisor.autorestart')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('supervisor.last_started')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                                        </Table.Row>
                                    </Table.Header>
                                    <Table.Body>
                                        {processes.map(item => (
                                            <Table.Row key={item.id}>
                                                <Table.Cell>
                                                    <Text weight="bold">{item.name}</Text>
                                                    {item.working_dir && <Text size="1" color="gray" as="div">{item.working_dir}</Text>}
                                                </Table.Cell>
                                                <Table.Cell>{statusBadge(item.status, t)}</Table.Cell>
                                                <Table.Cell>{item.pid || '-'}</Table.Cell>
                                                <Table.Cell><Text size="2" title={item.command}>{item.command?.slice(0, 80)}</Text></Table.Cell>
                                                <Table.Cell>{item.autostart ? t('common.yes') : t('common.no')}</Table.Cell>
                                                <Table.Cell>{item.autorestart ? t('common.yes') : t('common.no')}</Table.Cell>
                                                <Table.Cell>{formatDate(item.last_started_at)}</Table.Cell>
                                                <Table.Cell>
                                                    <Flex gap="1" wrap="wrap">
                                                        {!['running', 'starting', 'stopping'].includes(item.status) && (
                                                            <Button size="1" variant="soft" onClick={() => runAction('start', item.id)}><Play size={14} /></Button>
                                                        )}
                                                        {['running', 'starting', 'stopping'].includes(item.status) && (
                                                            <Button size="1" variant="soft" color="orange" onClick={() => runAction('stop', item.id)}><Square size={14} /></Button>
                                                        )}
                                                        {item.status === 'running' && (
                                                            <Button size="1" variant="soft" onClick={() => runAction('restart', item.id)}><RotateCw size={14} /></Button>
                                                        )}
                                                        <Button size="1" variant="soft" onClick={() => showLogs(item)}>{t('supervisor.logs')}</Button>
                                                        <Button size="1" variant="soft" onClick={() => openEdit(item)}><Pencil size={14} /></Button>
                                                        <Button size="1" variant="soft" color="red" onClick={() => setDeleteConfirm(item)}><Trash2 size={14} /></Button>
                                                    </Flex>
                                                </Table.Cell>
                                            </Table.Row>
                                        ))}
                                    </Table.Body>
                                </Table.Root>
                            )}
                        </Card>
                    </Tabs.Content>

                    <Tabs.Content value="logs">
                        <Card>
                            <Flex justify="between" align="center" mb="3">
                                <Text weight="bold">{logProcessId ? t('supervisor.process_logs') : t('supervisor.all_logs')}</Text>
                                <Button size="2" variant="soft" onClick={() => showLogs(null)}>{t('supervisor.all_logs')}</Button>
                            </Flex>
                            {logs.length === 0 ? (
                                <Text color="gray">{t('supervisor.no_logs')}</Text>
                            ) : (
                                <ScrollArea style={{ height: 420 }}>
                                    <Table.Root size="2">
                                        <Table.Header>
                                            <Table.Row>
                                                <Table.ColumnHeaderCell>{t('common.time')}</Table.ColumnHeaderCell>
                                                <Table.ColumnHeaderCell>{t('supervisor.process_name')}</Table.ColumnHeaderCell>
                                                <Table.ColumnHeaderCell>{t('supervisor.stream')}</Table.ColumnHeaderCell>
                                                <Table.ColumnHeaderCell>{t('supervisor.log_line')}</Table.ColumnHeaderCell>
                                            </Table.Row>
                                        </Table.Header>
                                        <Table.Body>
                                            {logs.map(log => (
                                                <Table.Row key={log.id}>
                                                    <Table.Cell>{formatDate(log.created_at)}</Table.Cell>
                                                    <Table.Cell>{log.name || '-'}</Table.Cell>
                                                    <Table.Cell>{streamBadge(log.stream)}</Table.Cell>
                                                    <Table.Cell><Text style={{ whiteSpace: 'pre-wrap' }}>{log.line}</Text></Table.Cell>
                                                </Table.Row>
                                            ))}
                                        </Table.Body>
                                    </Table.Root>
                                </ScrollArea>
                            )}
                        </Card>
                    </Tabs.Content>
                </Box>
            </Tabs.Root>

            <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
                <Dialog.Content maxWidth="720px">
                    <Dialog.Title>{editId ? t('supervisor.edit_process') : t('supervisor.new_process')}</Dialog.Title>
                    <Flex direction="column" gap="3">
                        <Box>
                            <Text size="2" weight="medium">{t('supervisor.process_name')}</Text>
                            <TextField.Root value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="my-worker" />
                        </Box>
                        <Box>
                            <Text size="2" weight="medium">{t('supervisor.command')}</Text>
                            <TextArea value={form.command} onChange={e => setForm({ ...form, command: e.target.value })} placeholder="npm start" rows={3} />
                        </Box>
                        <Box>
                            <Text size="2" weight="medium">{t('supervisor.working_dir')}</Text>
                            <TextField.Root value={form.working_dir} onChange={e => setForm({ ...form, working_dir: e.target.value })} placeholder="/srv/app" />
                        </Box>
                        <Box>
                            <Text size="2" weight="medium">{t('supervisor.env')}</Text>
                            <TextArea value={form.env} onChange={e => setForm({ ...form, env: e.target.value })} placeholder={'NODE_ENV=production\nPORT=3000'} rows={4} />
                        </Box>
                        <Flex gap="5" wrap="wrap">
                            <label><Flex gap="2" align="center"><Switch checked={form.enabled} onCheckedChange={v => setForm({ ...form, enabled: v })} />{t('supervisor.enabled')}</Flex></label>
                            <label><Flex gap="2" align="center"><Switch checked={form.autostart} onCheckedChange={v => setForm({ ...form, autostart: v })} />{t('supervisor.autostart')}</Flex></label>
                            <label><Flex gap="2" align="center"><Switch checked={form.autorestart} onCheckedChange={v => setForm({ ...form, autorestart: v })} />{t('supervisor.autorestart')}</Flex></label>
                        </Flex>
                        <Flex gap="3" wrap="wrap">
                            <Box style={{ flex: 1, minWidth: 160 }}>
                                <Text size="2" weight="medium">{t('supervisor.restart_delay')}</Text>
                                <TextField.Root type="number" value={form.restart_delay_sec} onChange={e => setForm({ ...form, restart_delay_sec: e.target.value })} />
                            </Box>
                            <Box style={{ flex: 1, minWidth: 160 }}>
                                <Text size="2" weight="medium">{t('supervisor.stop_timeout')}</Text>
                                <TextField.Root type="number" value={form.stop_timeout_sec} onChange={e => setForm({ ...form, stop_timeout_sec: e.target.value })} />
                            </Box>
                            <Box style={{ flex: 1, minWidth: 160 }}>
                                <Text size="2" weight="medium">{t('supervisor.max_restarts')}</Text>
                                <TextField.Root type="number" value={form.max_restarts} onChange={e => setForm({ ...form, max_restarts: e.target.value })} />
                            </Box>
                        </Flex>
                    </Flex>
                    <Flex gap="3" justify="end" mt="5">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleSave} disabled={saving}>{saving ? t('common.saving') : t('common.save')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            <Dialog.Root open={!!deleteConfirm} onOpenChange={(open) => !open && setDeleteConfirm(null)}>
                <Dialog.Content maxWidth="420px">
                    <Dialog.Title>{t('common.confirm_delete')}</Dialog.Title>
                    <Text>{t('supervisor.confirm_delete', { name: deleteConfirm?.name })}</Text>
                    <Flex gap="3" justify="end" mt="5">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button color="red" onClick={handleDelete}>{t('common.delete')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
