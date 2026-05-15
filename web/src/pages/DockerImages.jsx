import { useCallback, useEffect, useMemo, useState } from 'react'
import { Box, Flex, Text, Card, Heading, Button, TextField, Table, Badge, Callout, Tooltip, Dialog } from '@radix-ui/themes'
import { ArrowLeft, Check, Download, Image, RefreshCw, Search, Star, Trash2, X, PackageX } from 'lucide-react'
import { useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'
import { dockerAPI } from '../api/index.js'
import DockerRequired from '../components/DockerRequired.jsx'

function formatBytes(bytes) {
    if (!bytes) return '-'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let value = bytes
    let idx = 0
    while (value >= 1024 && idx < units.length - 1) {
        value /= 1024
        idx += 1
    }
    return `${value.toFixed(value >= 10 || idx === 0 ? 0 : 1)} ${units[idx]}`
}

function formatDate(ts) {
    if (!ts) return '-'
    const d = new Date(ts * 1000)
    if (Number.isNaN(d.getTime())) return '-'
    return d.toLocaleString()
}

function imageName(image) {
    const tags = image.tags || []
    const valid = tags.filter(tag => tag && tag !== '<none>:<none>')
    return valid[0] || image.id
}

function shortImageId(id = '') {
    return String(id).replace(/^sha256:/, '').slice(0, 12) || '-'
}

export default function DockerImages() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const [dockerStatus, setDockerStatus] = useState(null)
    const [dockerChecking, setDockerChecking] = useState(true)
    const [statusError, setStatusError] = useState(false)
    const [images, setImages] = useState([])
    const [loading, setLoading] = useState(true)
    const [actionLoading, setActionLoading] = useState('')
    const [error, setError] = useState('')
    const [pullImage, setPullImage] = useState('')
    const [searchQuery, setSearchQuery] = useState('')
    const [searchResults, setSearchResults] = useState([])
    const [searching, setSearching] = useState(false)
    const [filter, setFilter] = useState('all')
    const [selected, setSelected] = useState({})
    const [pullOpen, setPullOpen] = useState(false)

    const checkDocker = useCallback(async () => {
        setDockerChecking(true)
        try {
            const res = await dockerAPI.status()
            setDockerStatus(res.data)
            setStatusError(false)
        } catch {
            setDockerStatus(null)
            setStatusError(true)
            setLoading(false)
        } finally {
            setDockerChecking(false)
        }
    }, [])

    const loadImages = useCallback(async () => {
        setLoading(true)
        setError('')
        try {
            const res = await dockerAPI.listImages()
            setImages(res.data?.images || [])
            setSelected({})
        } catch (err) {
            setError(err.response?.data?.error || err.message)
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => { checkDocker() }, [checkDocker])
    useEffect(() => {
        if (dockerStatus?.installed && dockerStatus?.daemon_running) loadImages()
    }, [dockerStatus?.installed, dockerStatus?.daemon_running, loadImages])

    const totalSize = useMemo(() => images.reduce((sum, img) => sum + (img.size || 0), 0), [images])
    const filteredImages = useMemo(() => images.filter((img) => {
        if (filter === 'used') return !!img.used
        if (filter === 'unused') return !img.used
        return true
    }), [images, filter])
    const selectedIds = useMemo(() => Object.keys(selected).filter((id) => selected[id]), [selected])
    const allVisibleSelected = filteredImages.length > 0 && filteredImages.every((img) => selected[img.id])

    const handlePull = async (name = pullImage) => {
        const value = name.trim()
        if (!value) return
        setActionLoading(`pull:${value}`)
        setError('')
        try {
            await dockerAPI.pullImage(value)
            setPullImage('')
            setSearchResults([])
            setPullOpen(false)
            await loadImages()
        } catch (err) {
            setError(err.response?.data?.error || err.message)
        } finally {
            setActionLoading('')
        }
    }

    const handleSearch = async () => {
        const value = searchQuery.trim()
        if (!value) return
        setSearching(true)
        setError('')
        try {
            const res = await dockerAPI.searchImages(value, 20)
            setSearchResults(res.data?.results || [])
        } catch (err) {
            setError(err.response?.data?.error || err.message)
            setSearchResults([])
        } finally {
            setSearching(false)
        }
    }

    const handleRemove = async (image) => {
        const target = imageName(image)
        if (!confirm(t('docker.confirm_remove_image', { image: target }))) return
        setActionLoading(`remove:${image.id}`)
        setError('')
        try {
            await dockerAPI.removeImage(image.id)
            await loadImages()
        } catch (err) {
            setError(err.response?.data?.error || err.message)
        } finally {
            setActionLoading('')
        }
    }

    const handleBatchRemove = async () => {
        if (selectedIds.length === 0) return
        if (!confirm(t('docker.confirm_remove_selected_images', { count: selectedIds.length }))) return
        setActionLoading('batch-remove')
        setError('')
        try {
            for (const id of selectedIds) {
                await dockerAPI.removeImage(id)
            }
            await loadImages()
        } catch (err) {
            setError(err.response?.data?.error || err.message)
        } finally {
            setActionLoading('')
        }
    }

    const toggleAllVisible = () => {
        setSelected((prev) => {
            const next = { ...prev }
            filteredImages.forEach((img) => { next[img.id] = !allVisibleSelected })
            return next
        })
    }

    const handlePrune = async () => {
        if (!confirm(t('docker.confirm_prune_images'))) return
        setActionLoading('prune')
        setError('')
        try {
            const res = await dockerAPI.pruneImages()
            await loadImages()
            const reclaimed = formatBytes(res.data?.space_reclaimed || 0)
            setError(`${t('docker.pruned')}: ${reclaimed}`)
        } catch (err) {
            setError(err.response?.data?.error || err.message)
        } finally {
            setActionLoading('')
        }
    }

    if (dockerChecking || loading) {
        return <Flex align="center" justify="center" style={{ minHeight: 200 }}><RefreshCw size={20} className="spin" /><Text ml="2">{t('common.loading')}</Text></Flex>
    }

    if (statusError) {
        return (
            <Box>
                <Flex align="center" gap="2" mb="4">
                    <Image size={24} />
                    <Heading size="5">{t('docker.image_management')}</Heading>
                </Flex>
                <Callout.Root color="orange">
                    <Callout.Icon><RefreshCw size={16} /></Callout.Icon>
                    <Box>
                        <Text size="2" weight="bold" style={{ display: 'block' }}>{t('docker.status_error_title')}</Text>
                        <Text size="2">{t('docker.status_error_desc')}</Text>
                    </Box>
                </Callout.Root>
                <Flex mt="3">
                    <Button variant="soft" onClick={checkDocker}><RefreshCw size={16} /> {t('docker.check_again')}</Button>
                </Flex>
            </Box>
        )
    }

    if (dockerStatus && (!dockerStatus.installed || !dockerStatus.daemon_running)) {
        return (
            <Box>
                <Flex align="center" gap="2" mb="4">
                    <Image size={24} />
                    <Heading size="5">{t('docker.image_management')}</Heading>
                </Flex>
                <DockerRequired
                    installed={dockerStatus.installed}
                    daemonRunning={dockerStatus.daemon_running}
                    error={dockerStatus.error}
                    runtime={dockerStatus.runtime}
                    onRetry={checkDocker}
                />
            </Box>
        )
    }

    return (
        <Box>
                <Flex align="center" justify="between" mb="4" wrap="wrap" gap="2">
                    <Flex align="center" gap="2">
                        <Button size="2" variant="ghost" color="gray" onClick={() => navigate('/docker')}>
                            <ArrowLeft size={16} />
                        </Button>
                    <Image size={24} />
                    <Heading size="5">{t('docker.image_management')}</Heading>
                    <Badge variant="soft" size="1">{images.length}</Badge>
                    </Flex>
                    <Flex gap="2" wrap="wrap">
                        <Button size="2" variant="soft" onClick={() => setPullOpen(true)}>
                            <Download size={16} /> {t('docker.add_image')}
                        </Button>
                    <Button size="2" variant="soft" onClick={loadImages} disabled={!!actionLoading}>
                        <RefreshCw size={16} /> {t('common.refresh')}
                    </Button>
                    <Button size="2" variant="soft" color="red" onClick={handleBatchRemove} disabled={selectedIds.length === 0 || !!actionLoading}>
                        <Trash2 size={16} /> {t('docker.delete_selected')} ({selectedIds.length})
                    </Button>
                    <Button size="2" variant="soft" color="red" onClick={handlePrune} disabled={!!actionLoading}>
                        <PackageX size={16} /> {t('docker.prune')}
                    </Button>
                </Flex>
            </Flex>

            {error && (
                <Callout.Root color={error.startsWith(t('docker.pruned')) ? 'green' : 'red'} mb="3">
                    <Callout.Icon>{error.startsWith(t('docker.pruned')) ? <Check size={16} /> : <X size={16} />}</Callout.Icon>
                    <Callout.Text>{error}</Callout.Text>
                </Callout.Root>
            )}

            <Flex gap="3" mb="4" wrap="wrap">
                <Card style={{ padding: '12px 16px', flex: 1, minWidth: 160 }}>
                    <Text size="1" color="gray">{t('docker.images')}</Text>
                    <Text size="4" weight="bold" style={{ display: 'block' }}>{images.length}</Text>
                </Card>
                <Card style={{ padding: '12px 16px', flex: 1, minWidth: 160 }}>
                    <Text size="1" color="gray">{t('docker.image_total_size')}</Text>
                    <Text size="4" weight="bold" style={{ display: 'block' }}>{formatBytes(totalSize)}</Text>
                </Card>
            </Flex>

            <Dialog.Root open={pullOpen} onOpenChange={setPullOpen}>
                <Dialog.Content maxWidth="640px" aria-describedby={undefined}>
                    <Dialog.Title>{t('docker.pull_image')}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                    <Flex gap="2" wrap="wrap">
                        <TextField.Root
                            id="docker-pull-image-input"
                            value={pullImage}
                            onChange={(e) => setPullImage(e.target.value)}
                            onKeyDown={(e) => { if (e.key === 'Enter') handlePull() }}
                            placeholder="nginx:latest"
                            style={{ flex: '1 1 320px' }}
                        />
                        <Button onClick={() => handlePull()} disabled={!pullImage.trim() || !!actionLoading}>
                            <Download size={16} /> {actionLoading.startsWith('pull:') ? t('docker.pulling') : t('docker.pull')}
                        </Button>
                    </Flex>
                    <Flex gap="2" wrap="wrap">
                        <TextField.Root
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            onKeyDown={(e) => { if (e.key === 'Enter') handleSearch() }}
                            placeholder={t('docker.search_placeholder')}
                            style={{ flex: '1 1 320px' }}
                        />
                        <Button variant="soft" onClick={handleSearch} disabled={!searchQuery.trim() || searching}>
                            <Search size={16} /> {t('docker.search_hub')}
                        </Button>
                    </Flex>
                    {searchResults.length > 0 && (
                        <Box style={{ border: '1px solid var(--gray-5)', borderRadius: 8, overflow: 'hidden' }}>
                            {searchResults.map((r, i) => (
                                <Flex key={r.name} align="center" gap="2" px="3" py="2" style={{ borderBottom: i < searchResults.length - 1 ? '1px solid var(--gray-4)' : 'none' }}>
                                    <Box style={{ flex: 1, minWidth: 0 }}>
                                        <Flex align="center" gap="2" wrap="wrap">
                                            <Text size="2" weight="bold">{r.name}</Text>
                                            {r.is_official && <Badge color="blue" size="1">{t('docker.official')}</Badge>}
                                            <Flex align="center" gap="1"><Star size={12} /><Text size="1">{r.star_count}</Text></Flex>
                                        </Flex>
                                        <Text size="1" color="gray" style={{ display: 'block' }}>{r.description}</Text>
                                    </Box>
                                    <Button size="1" variant="soft" onClick={() => handlePull(r.name)} disabled={!!actionLoading}>
                                        <Download size={12} /> {t('docker.pull')}
                                    </Button>
                                </Flex>
                            ))}
                        </Box>
                    )}
                    </Flex>
                    <Flex justify="end" gap="2" mt="3">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            <Card>
                <Flex align="center" justify="between" mb="3" wrap="wrap" gap="2">
                    <Text size="3" weight="bold">{t('docker.images')}</Text>
                    <Flex align="center" gap="3" wrap="wrap">
                        <Text size="1" color="gray">{t('docker.filtered_count', { count: filteredImages.length, total: images.length })}</Text>
                        <Flex gap="2" wrap="wrap" align="center">
                            {['all', 'used', 'unused'].map((key) => (
                                <Button
                                    key={key}
                                    size="1"
                                    variant={filter === key ? 'solid' : 'soft'}
                                    onClick={() => setFilter(key)}
                                >
                                    {t(`docker.image_filter_${key}`)}
                                </Button>
                            ))}
                        </Flex>
                    </Flex>
                </Flex>
                {filteredImages.length === 0 ? (
                    <Flex direction="column" align="center" gap="3" py="6">
                        <Image size={40} color="var(--gray-8)" />
                        <Text color="gray">{t('docker.no_images')}</Text>
                    </Flex>
                ) : (
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>
                                    <input type="checkbox" checked={allVisibleSelected} onChange={toggleAllVisible} aria-label={t('docker.select_all')} />
                                </Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('docker.image')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('docker.tags')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('docker.size')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('docker.created_at')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {filteredImages.map((img) => (
                                <Table.Row key={img.id}>
                                    <Table.Cell>
                                        <input
                                            type="checkbox"
                                            checked={!!selected[img.id]}
                                            onChange={(e) => setSelected((prev) => ({ ...prev, [img.id]: e.target.checked }))}
                                            aria-label={imageName(img)}
                                        />
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Text weight="medium">{shortImageId(img.id)}</Text>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Flex gap="1" wrap="wrap">
                                            {(img.tags || []).filter(tag => tag !== '<none>:<none>').slice(0, 4).map(tag => (
                                                <Badge key={tag} variant="soft" color="gray">{tag}</Badge>
                                            ))}
                                            {(img.tags || []).length > 4 && <Badge variant="outline">+{img.tags.length - 4}</Badge>}
                                        </Flex>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Tooltip content={(img.used_by || []).join(', ') || t('docker.unused')}>
                                            <Badge color={img.used ? 'green' : 'gray'} variant="soft">
                                                {img.used ? t('docker.used') : t('docker.unused')}
                                            </Badge>
                                        </Tooltip>
                                    </Table.Cell>
                                    <Table.Cell>{formatBytes(img.size)}</Table.Cell>
                                    <Table.Cell>{formatDate(img.created)}</Table.Cell>
                                    <Table.Cell>
                                        <Tooltip content={t('common.delete')}>
                                            <Button size="1" variant="ghost" color="red" onClick={() => handleRemove(img)} disabled={!!actionLoading}>
                                                <Trash2 size={14} />
                                            </Button>
                                        </Tooltip>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                )}
            </Card>
        </Box>
    )
}
