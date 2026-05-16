import { useState, useEffect, useCallback, useMemo, Fragment } from 'react'
import {
    Box, Flex, Text, Card, Badge, Button, Table, Dialog, TextField,
    Switch, TextArea, Heading, Callout, ScrollArea, Tabs,
    Select, Spinner,
} from '@radix-ui/themes'
import {
    Clock, Play, Plus, Trash2,
    Timer, RotateCcw, ChevronDown, ChevronUp, ArrowLeft,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cronjobAPI, databaseAPI } from '../api/index.js'

function statusBadge(status, t) {
    const statusMap = {
        success: { color: 'green', label: t('cronjob.status_success') },
        failed: { color: 'red', label: t('cronjob.status_failed') },
        timeout: { color: 'orange', label: t('cronjob.status_timeout') },
        skipped: { color: 'gray', label: t('cronjob.status_skipped') },
        running: { color: 'blue', label: t('cronjob.status_running') },
    }
    const m = statusMap[status] || { color: 'gray', label: status || '-' }
    return <Badge color={m.color} variant="soft">{m.label}</Badge>
}

function formatDate(ts) {
    if (!ts) return '-'
    const d = new Date(ts)
    if (isNaN(d.getTime())) return '-'
    return d.toLocaleString()
}

function formatDuration(ms) {
    if (ms == null) return '-'
    if (ms < 1000) return `${ms}ms`
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
    return `${Math.floor(ms / 60000)}m ${Math.round((ms % 60000) / 1000)}s`
}

const defaultForm = {
    name: '', expression: '', type: 'shell', command: '', working_dir: '', database_instance_id: '',
    timeout_sec: 300, max_retries: 0, notify_on_failure: false, enabled: true,
}

export default function CronJobManager() {
    const { t } = useTranslation()
    const [tasks, setTasks] = useState([])
    const [logs, setLogs] = useState([])
    const [databaseInstances, setDatabaseInstances] = useState([])
    const [loading, setLoading] = useState(true)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [editId, setEditId] = useState(null)
    const [form, setForm] = useState({ ...defaultForm })
    const [saving, setSaving] = useState(false)
    const [triggerConfirm, setTriggerConfirm] = useState(null)
    const [deleteConfirm, setDeleteConfirm] = useState(null)
    const [expandedLog, setExpandedLog] = useState(null)
    const [activeTab, setActiveTab] = useState('tasks')
    const [logTaskId, setLogTaskId] = useState(null)
    const selectedLogTask = useMemo(
        () => tasks.find(task => task.id === logTaskId),
        [tasks, logTaskId]
    )
    const hasRunningTasks = useMemo(
        () => tasks.some(task => task.last_status === 'running'),
        [tasks]
    )

    const fetchTasks = useCallback(async () => {
        try {
            const res = await cronjobAPI.listTasks()
            setTasks(res.data || [])
        } catch { /* ignore */ }
    }, [])

    const fetchLogs = useCallback(async (taskId, opts = {}) => {
        try {
            const res = taskId
                ? await cronjobAPI.taskLogs(taskId, 50)
                : await cronjobAPI.allLogs(50)
            const nextLogs = res.data || []
            setLogs(nextLogs)
            if (opts.expandLatest) {
                setExpandedLog(nextLogs[0]?.id || null)
            }
        } catch { /* ignore */ }
    }, [])

    const fetchDatabaseInstances = useCallback(async () => {
        try {
            const res = await databaseAPI.listInstances()
            const instances = Array.isArray(res.data) ? res.data : (res.data?.instances || [])
            setDatabaseInstances(instances.filter(inst => String(inst.engine || '').toLowerCase() !== 'redis'))
        } catch {
            setDatabaseInstances([])
        }
    }, [])

    const fetchAll = useCallback(async (silent = false) => {
        if (!silent) setLoading(true)
        await fetchTasks()
        await fetchLogs(logTaskId)
        await fetchDatabaseInstances()
        if (!silent) setLoading(false)
    }, [fetchTasks, fetchLogs, fetchDatabaseInstances, logTaskId])

    useEffect(() => { fetchAll() }, [fetchAll])

    useEffect(() => {
        if (!hasRunningTasks) return undefined
        const timer = window.setInterval(() => {
            fetchAll(true)
        }, 2000)
        return () => window.clearInterval(timer)
    }, [fetchAll, hasRunningTasks])

    const openCreate = () => {
        setEditId(null)
        setForm({ ...defaultForm })
        fetchDatabaseInstances()
        setDialogOpen(true)
    }

    const openEdit = (task) => {
        setEditId(task.id)
        fetchDatabaseInstances()
        let databaseInstanceId = ''
        if (task.type === 'database_backup' && task.payload) {
            try {
                databaseInstanceId = String(JSON.parse(task.payload).instance_id || '')
            } catch { /* ignore */ }
        }
        setForm({
            name: task.name || '',
            expression: task.expression || '',
            type: task.type || 'shell',
            command: task.command || '',
            working_dir: task.working_dir || '',
            database_instance_id: databaseInstanceId,
            timeout_sec: task.timeout_sec || 300,
            max_retries: task.max_retries || 0,
            notify_on_failure: !!task.notify_on_failure,
            enabled: task.enabled !== false,
        })
        setDialogOpen(true)
    }

    const handleSave = async () => {
        setSaving(true)
        try {
            const data = {
                ...form,
                tags: [],
                timeout_sec: Number(form.timeout_sec) || 300,
                max_retries: Number(form.max_retries) || 0,
            }
            if (form.type === 'database_backup') {
                data.command = ''
                data.working_dir = ''
                data.payload = JSON.stringify({ instance_id: Number(form.database_instance_id) || 0 })
            } else {
                data.payload = ''
            }
            delete data.database_instance_id
            if (editId) {
                await cronjobAPI.updateTask(editId, data)
            } else {
                await cronjobAPI.createTask(data)
            }
            setDialogOpen(false)
            fetchAll()
        } catch (e) {
            alert(e?.response?.data?.error || e.message)
        } finally {
            setSaving(false)
        }
    }

    const handleTrigger = async (id) => {
        try {
            await cronjobAPI.triggerTask(id)
            setTriggerConfirm(null)
            setTasks(prev => prev.map(task => (
                task.id === id
                    ? { ...task, last_status: 'running', last_run_at: new Date().toISOString() }
                    : task
            )))
            setTimeout(() => fetchAll(true), 1000)
        } catch (e) {
            alert(e?.response?.data?.error || e.message)
        }
    }

    const handleDelete = async (id) => {
        try {
            await cronjobAPI.deleteTask(id)
            setDeleteConfirm(null)
            setDialogOpen(false)
            setEditId(null)
            if (logTaskId === id) {
                setLogTaskId(null)
                setExpandedLog(null)
            }
            fetchAll()
        } catch (e) {
            alert(e?.response?.data?.error || e.message)
        }
    }

    const viewTaskLogs = (taskId) => {
        setLogTaskId(taskId)
        setActiveTab('logs')
        fetchLogs(taskId, { expandLatest: true })
    }

    const viewAllLogs = () => {
        setLogTaskId(null)
        setExpandedLog(null)
        fetchLogs(null)
    }

    const handleTabChange = (value) => {
        setActiveTab(value)
        if (value === 'logs') {
            viewAllLogs()
        }
    }

    const backToTasks = () => {
        setLogTaskId(null)
        setExpandedLog(null)
        setActiveTab('tasks')
    }

    const taskTypeLabel = (type) => {
        if (type === 'database_backup') return t('cronjob.type_database_backup')
        return t('cronjob.type_shell')
    }

    const canSave = form.name && form.expression && (
        form.type === 'database_backup' ? form.database_instance_id : form.command
    )

    return (
        <Box p="4" style={{ maxWidth: 1200, margin: '0 auto' }}>
            <Flex justify="between" align="center" mb="4">
                <Box>
                    <Heading size="5"><Clock size={20} style={{ display: 'inline', marginRight: 8, verticalAlign: 'text-bottom' }} />{t('cronjob.title')}</Heading>
                    <Text size="2" color="gray">{t('cronjob.subtitle')}</Text>
                </Box>
                <Flex gap="2">
                    <Button variant="soft" onClick={fetchAll}><RotateCcw size={14} /></Button>
                    <Button onClick={openCreate}><Plus size={14} /> {t('cronjob.new_task')}</Button>
                </Flex>
            </Flex>

            <Tabs.Root value={activeTab} onValueChange={handleTabChange}>
                {!logTaskId && (
                    <Tabs.List>
                        <Tabs.Trigger value="tasks"><Clock size={14} style={{ marginRight: 4 }} />{t('cronjob.title')}</Tabs.Trigger>
                        <Tabs.Trigger value="logs"><Timer size={14} style={{ marginRight: 4 }} />{t('cronjob.logs')}</Tabs.Trigger>
                    </Tabs.List>
                )}

                <Tabs.Content value="tasks">
                    <Card mt="3">
                        {tasks.length === 0 ? (
                            <Callout.Root color="gray" mt="2">
                                <Callout.Text>{t('cronjob.no_tasks')}</Callout.Text>
                            </Callout.Root>
                        ) : (
                            <Table.Root>
                                <Table.Header>
                                    <Table.Row>
                                        <Table.ColumnHeaderCell>{t('cronjob.task_name')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.expression')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.next_run')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.last_status')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.actions')}</Table.ColumnHeaderCell>
                                    </Table.Row>
                                </Table.Header>
                                <Table.Body>
                                    {tasks.map(task => {
                                        const isRunning = task.last_status === 'running'
                                        return (
                                            <Table.Row
                                                key={task.id}
                                                onClick={() => openEdit(task)}
                                                onKeyDown={(e) => {
                                                    if (e.key === 'Enter' || e.key === ' ') {
                                                        e.preventDefault()
                                                        openEdit(task)
                                                    }
                                                }}
                                                tabIndex={0}
                                                style={{ cursor: 'pointer' }}
                                            >
                                                <Table.Cell>
                                                    <Flex align="center" gap="2" wrap="wrap">
                                                        <Text weight="medium">{task.name}</Text>
                                                        <Badge size="1" variant="soft" color={task.type === 'database_backup' ? 'blue' : 'gray'} style={{ width: 'fit-content' }}>
                                                            {taskTypeLabel(task.type)}
                                                        </Badge>
                                                    </Flex>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <code style={{ fontSize: 12 }}>{task.expression}</code>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="1" color="gray">{formatDate(task.next_run_at)}</Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Flex align="center" gap="2" wrap="wrap">
                                                        {statusBadge(task.last_status, t)}
                                                        <Text size="1" color="gray">{formatDate(task.last_run_at)}</Text>
                                                    </Flex>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Flex gap="1" onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
                                                        <Button size="1" variant="soft" color="green" disabled={isRunning} onClick={() => setTriggerConfirm(task.id)}>
                                                            {isRunning ? <Spinner size="1" /> : <Play size={14} />}
                                                            {isRunning ? t('cronjob.status_running') : t('cronjob.action_start')}
                                                        </Button>
                                                        <Button size="1" variant="soft" onClick={() => viewTaskLogs(task.id)}>
                                                            <Timer size={14} /> {t('cronjob.action_diary')}
                                                        </Button>
                                                    </Flex>
                                                </Table.Cell>
                                            </Table.Row>
                                        )
                                    })}
                                </Table.Body>
                            </Table.Root>
                        )}
                    </Card>
                </Tabs.Content>

                <Tabs.Content value="logs">
                    <Card mt="3">
                        <Flex justify="between" align="center" mb="3">
                            <Box>
                                <Text weight="medium">
                                    {logTaskId
                                        ? t('cronjob.task_diary_title', {
                                            name: selectedLogTask?.name || t('cronjob.unknown_task', { id: logTaskId }),
                                        })
                                        : t('cronjob.all_logs')}
                                </Text>
                                {logTaskId && (
                                    <Text size="2" color="gray" style={{ display: 'block', marginTop: 2 }}>
                                        {t('cronjob.task_diary_hint', {
                                            name: selectedLogTask?.name || t('cronjob.unknown_task', { id: logTaskId }),
                                        })}
                                    </Text>
                                )}
                            </Box>
                            {logTaskId && (
                                <Button size="1" variant="soft" onClick={backToTasks}>
                                    <ArrowLeft size={14} />
                                    {t('common.back')}
                                </Button>
                            )}
                        </Flex>
                        {logs.length === 0 ? (
                            <Callout.Root color="gray">
                                <Callout.Text>{t('cronjob.no_logs')}</Callout.Text>
                            </Callout.Root>
                        ) : (
                            <Table.Root>
                                <Table.Header>
                                    <Table.Row>
                                        <Table.ColumnHeaderCell>{t('cronjob.task_name')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_started')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_duration')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_status')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_exit_code')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_output')}</Table.ColumnHeaderCell>
                                    </Table.Row>
                                </Table.Header>
                                <Table.Body>
                                    {logs.map(log => (
                                        <Fragment key={log.id}>
                                            <Table.Row>
                                                <Table.Cell><Text size="2">{log.task_name || `#${log.task_id}`}</Text></Table.Cell>
                                                <Table.Cell><Text size="1">{formatDate(log.started_at)}</Text></Table.Cell>
                                                <Table.Cell><Text size="2">{formatDuration(log.duration_ms)}</Text></Table.Cell>
                                                <Table.Cell>{statusBadge(log.status, t)}</Table.Cell>
                                                <Table.Cell><Text size="2">{log.exit_code}</Text></Table.Cell>
                                                <Table.Cell>
                                                    <Button size="1" variant="ghost" onClick={() => setExpandedLog(expandedLog === log.id ? null : log.id)}>
                                                        {expandedLog === log.id ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
                                                    </Button>
                                                </Table.Cell>
                                            </Table.Row>
                                            {expandedLog === log.id && (
                                                <Table.Row>
                                                    <Table.Cell colSpan={6}>
                                                        <Card style={{ background: 'var(--gray-2)' }}>
                                                            <ScrollArea style={{ maxHeight: 300 }}>
                                                                <pre style={{ fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: 0 }}>{log.output || '-'}</pre>
                                                            </ScrollArea>
                                                        </Card>
                                                    </Table.Cell>
                                                </Table.Row>
                                            )}
                                        </Fragment>
                                    ))}
                                </Table.Body>
                            </Table.Root>
                        )}

                    </Card>
                </Tabs.Content>
            </Tabs.Root>

            {/* Create / Edit Dialog */}
            <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
                <Dialog.Content style={{ maxWidth: 520 }} aria-describedby={undefined}>
                    <Dialog.Title>{editId ? t('cronjob.edit_task') : t('cronjob.new_task')}</Dialog.Title>

                    <Flex direction="column" gap="3" mt="3">
                        <label>
                            <Text size="2" weight="medium">{t('cronjob.task_name')}</Text>
                            <TextField.Root mt="1" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder={t('cronjob.task_name_placeholder')} />
                        </label>

                        <Box>
                            <Text size="2" weight="medium">{t('cronjob.task_type')}</Text>
                            <Select.Root
                                value={form.type}
                                onValueChange={v => {
                                    if (v === 'database_backup') fetchDatabaseInstances()
                                    setForm({
                                        ...form,
                                        type: v,
                                        timeout_sec: v === 'database_backup' && Number(form.timeout_sec) < 1800 ? 1800 : form.timeout_sec,
                                    })
                                }}
                            >
                                <Select.Trigger mt="1" style={{ width: '100%' }} />
                                <Select.Content>
                                    <Select.Item value="shell">{t('cronjob.type_shell')}</Select.Item>
                                    <Select.Item value="database_backup">{t('cronjob.type_database_backup')}</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Box>

                        <label>
                            <Text size="2" weight="medium">{t('cronjob.expression')}</Text>
                            <TextField.Root mt="1" value={form.expression} onChange={e => setForm({ ...form, expression: e.target.value })} placeholder="*/5 * * * *" />
                            <Text size="1" color="gray">{t('cronjob.expression_help')}</Text>
                            <Text size="1" color="gray" style={{ display: 'block' }}>{t('cronjob.expression_examples')}</Text>
                        </label>

                        {form.type === 'database_backup' ? (
                            <Box>
                                <Text size="2" weight="medium">{t('cronjob.database_instance')}</Text>
                                <select
                                    value={form.database_instance_id}
                                    onChange={e => setForm({ ...form, database_instance_id: e.target.value })}
                                    disabled={databaseInstances.length === 0}
                                    style={{
                                        width: '100%',
                                        height: 36,
                                        marginTop: 4,
                                        padding: '0 10px',
                                        borderRadius: 6,
                                        border: '1px solid var(--gray-7)',
                                        background: 'var(--color-panel)',
                                        color: 'var(--gray-12)',
                                        outline: 'none',
                                    }}
                                >
                                    <option value="">{databaseInstances.length === 0 ? t('cronjob.no_database_instances') : t('cronjob.select_database_instance')}</option>
                                    {databaseInstances.map(inst => (
                                        <option key={inst.id} value={String(inst.id)}>
                                            {inst.name} / {inst.engine} / {inst.source === 'remote' ? t('cronjob.remote_database') : t('cronjob.local_database')}
                                        </option>
                                    ))}
                                </select>
                                <Text size="1" color="gray">{t('cronjob.database_backup_help')}</Text>
                            </Box>
                        ) : (
                            <>
                                <label>
                                    <Text size="2" weight="medium">{t('cronjob.command')}</Text>
                                    <TextArea mt="1" value={form.command} onChange={e => setForm({ ...form, command: e.target.value })} placeholder={t('cronjob.command_placeholder')} rows={3} />
                                </label>

                                <label>
                                    <Text size="2" weight="medium">{t('cronjob.working_dir')}</Text>
                                    <TextField.Root mt="1" value={form.working_dir} onChange={e => setForm({ ...form, working_dir: e.target.value })} placeholder={t('cronjob.working_dir_placeholder')} />
                                </label>
                            </>
                        )}

                        <Flex gap="3">
                            <label style={{ flex: 1 }}>
                                <Text size="2" weight="medium">{t('cronjob.timeout')}</Text>
                                <TextField.Root mt="1" type="number" value={form.timeout_sec} onChange={e => setForm({ ...form, timeout_sec: e.target.value })} />
                            </label>
                            <label style={{ flex: 1 }}>
                                <Text size="2" weight="medium">{t('cronjob.max_retries')}</Text>
                                <TextField.Root mt="1" type="number" value={form.max_retries} onChange={e => setForm({ ...form, max_retries: e.target.value })} />
                            </label>
                        </Flex>

                        <Flex gap="4" align="center">
                            <label style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                                <Switch checked={form.notify_on_failure} onCheckedChange={v => setForm({ ...form, notify_on_failure: v })} />
                                <Text size="2">{t('cronjob.notify_on_failure')}</Text>
                            </label>
                            <label style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                                <Switch checked={form.enabled} onCheckedChange={v => setForm({ ...form, enabled: v })} />
                                <Text size="2">{t('cronjob.enabled')}</Text>
                            </label>
                        </Flex>
                    </Flex>

                    <Flex mt="4" justify="between" align="center" gap="3">
                        <Box>
                            {editId && (
                                <Button variant="soft" color="red" onClick={() => setDeleteConfirm(editId)}>
                                    <Trash2 size={14} /> {t('common.delete')}
                                </Button>
                            )}
                        </Box>
                        <Flex gap="3">
                            <Dialog.Close>
                                <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                            </Dialog.Close>
                            <Button onClick={handleSave} disabled={saving || !canSave}>
                                {saving ? t('common.loading') : t('common.save')}
                            </Button>
                        </Flex>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Trigger Confirm Dialog */}
            <Dialog.Root open={triggerConfirm != null} onOpenChange={() => setTriggerConfirm(null)}>
                <Dialog.Content style={{ maxWidth: 400 }} aria-describedby={undefined}>
                    <Dialog.Title>{t('cronjob.trigger')}</Dialog.Title>
                    <Text>{t('cronjob.trigger_confirm')}</Text>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button color="green" onClick={() => handleTrigger(triggerConfirm)}>
                            <Play size={14} /> {t('cronjob.trigger')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Delete Confirm Dialog */}
            <Dialog.Root open={deleteConfirm != null} onOpenChange={() => setDeleteConfirm(null)}>
                <Dialog.Content style={{ maxWidth: 400 }} aria-describedby={undefined}>
                    <Dialog.Title>{t('common.delete')}</Dialog.Title>
                    <Text>{t('cronjob.delete_confirm')}</Text>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button color="red" onClick={() => handleDelete(deleteConfirm)}>
                            <Trash2 size={14} /> {t('common.delete')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
