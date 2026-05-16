import { Box, Flex, Text, DropdownMenu, Separator, Dialog, Button, TextField, Spinner, Callout, Badge } from '@radix-ui/themes'
import { useState, useEffect, useCallback } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router'
import {
    LayoutDashboard,
    Globe,
    Settings,
    LogOut,
    User,
    ChevronDown,
    Sun,
    Moon,
    Languages,
    Menu,
    X,
    Box as BoxIcon,
    Database,
    FolderOpen,
    Shield,
    CalendarClock,
    SquareTerminal,
    ServerCog,
    Puzzle,
    KeyRound,
    AlertCircle,
    CheckCircle2,
    DownloadCloud,
    Store,
    LayoutTemplate,
    Monitor,
    Bot,
} from 'lucide-react'
import { useAuthStore } from '../stores/auth.js'
import { useThemeStore } from '../stores/theme.js'
import { usePluginNavStore } from '../stores/pluginNav.js'
import { authAPI, dashboardAPI, panelUpdateAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import Markdown from 'react-markdown'
import logoImg from '../assets/logo.png'
import AIChatWidget from './AIChatWidget.jsx'

const navItems = [
    { to: '/', icon: LayoutDashboard, labelKey: 'nav.dashboard', end: true },
    { to: '/hosts', icon: Globe, labelKey: 'nav.hosts' },
]

const pluginIcons = {
    Box: BoxIcon,
    Container: BoxIcon,
    Database,
    FolderOpen,
    Shield,
    CalendarClock,
    SquareTerminal,
    ServerCog,
    Store,
    LayoutTemplate,
    Bot,
}

const bottomNavItems = [
    { to: '/plugins', icon: Puzzle, labelKey: 'nav.plugins', labelFallback: '插件管理' },
    { to: '/settings', icon: Settings, labelKey: 'nav.settings' },
]

const releaseMarkdownComponents = {
    p: ({ node, ...props }) => <Text as="p" size="2" style={{ margin: '0 0 8px', lineHeight: 1.6 }} {...props} />,
    ul: ({ node, ...props }) => <ul style={{ margin: '0 0 8px 18px', padding: 0, lineHeight: 1.6 }} {...props} />,
    ol: ({ node, ...props }) => <ol style={{ margin: '0 0 8px 18px', padding: 0, lineHeight: 1.6 }} {...props} />,
    li: ({ node, ...props }) => <li style={{ marginBottom: 4 }} {...props} />,
    a: ({ node, ...props }) => <a target="_blank" rel="noreferrer" {...props} />,
}

function SidebarLink({ to, icon: Icon, label, end, onClick }) {
    return (
        <NavLink to={to} end={end} className="sidebar-link" onClick={onClick}>
            <Icon size={18} />
            <span>{label}</span>
        </NavLink>
    )
}

export default function Layout() {
    const navigate = useNavigate()
    const { t, i18n } = useTranslation()
    const { user, setToken, setUser, logout } = useAuthStore()
    const { mode: themeMode, theme, setMode: setThemeMode } = useThemeStore()
    const pluginNavItems = usePluginNavStore((s) => s.navItems)
    const plugins = usePluginNavStore((s) => s.plugins)
    const refreshPluginNav = usePluginNavStore((s) => s.refresh)
    const [version, setVersion] = useState('')
    const [isMobile, setIsMobile] = useState(() =>
        typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches
    )
    const [sidebarOpen, setSidebarOpen] = useState(false)
    const [changePasswordOpen, setChangePasswordOpen] = useState(false)
    const [savingPassword, setSavingPassword] = useState(false)
    const [profileMsg, setProfileMsg] = useState(null)
    const [profileForm, setProfileForm] = useState({
        username: '',
        current_password: '',
        new_password: '',
        confirm_password: '',
    })
    const [updateInfo, setUpdateInfo] = useState(null)
    const [updateDialogOpen, setUpdateDialogOpen] = useState(false)
    const [checkingUpdate, setCheckingUpdate] = useState(false)
    const [preparingUpdate, setPreparingUpdate] = useState(false)
    const [restartingUpdate, setRestartingUpdate] = useState(false)
    const [updateMsg, setUpdateMsg] = useState(null)

    const currentLang = i18n.language?.startsWith('zh') ? 'zh' : 'en'
    const aiEnabled = plugins.some((p) => p.id === 'ai' && p.enabled)
    const canManageUpdate = user?.role === 'owner' || user?.role === 'admin'

    const toggleLang = () => {
        const next = currentLang === 'zh' ? 'en' : 'zh'
        i18n.changeLanguage(next)
    }

    const refreshUpdateInfo = useCallback(async (silent = true) => {
        if (!silent) setCheckingUpdate(true)
        try {
            const res = await panelUpdateAPI.check()
            setUpdateInfo(res.data)
            return res.data
        } catch {
            if (!silent) {
                setUpdateMsg({ type: 'error', text: '检查更新失败，请稍后重试' })
            }
            return null
        } finally {
            if (!silent) setCheckingUpdate(false)
        }
    }, [])

    useEffect(() => {
        const mql = window.matchMedia('(max-width: 767px)')
        const handler = (e) => {
            setIsMobile(e.matches)
            if (!e.matches) setSidebarOpen(false)
        }
        mql.addEventListener('change', handler)
        return () => mql.removeEventListener('change', handler)
    }, [])

    useEffect(() => {
        dashboardAPI.stats().then(res => {
            setVersion(res.data?.system?.panel_version || '')
        }).catch(() => { })
    }, [])

    useEffect(() => {
        refreshUpdateInfo(true)
    }, [refreshUpdateInfo])

    useEffect(() => {
        refreshPluginNav()
    }, [refreshPluginNav])

    useEffect(() => {
        if (changePasswordOpen) {
            setProfileForm({
                username: user?.username || '',
                current_password: '',
                new_password: '',
                confirm_password: '',
            })
            setProfileMsg(null)
        }
    }, [changePasswordOpen, user?.username])

    const handleNavClick = useCallback(() => {
        if (isMobile) setSidebarOpen(false)
    }, [isMobile])

    const handleLogout = () => {
        if (isMobile) setSidebarOpen(false)
        logout()
        navigate('/login', { replace: true })
    }

    const handleUpdateDialogChange = (open) => {
        setUpdateDialogOpen(open)
        if (open) {
            setUpdateMsg(null)
            refreshUpdateInfo(false)
        } else {
            setUpdateMsg(null)
        }
    }

    const handlePrepareUpdate = async () => {
        if (!canManageUpdate) {
            setUpdateMsg({ type: 'error', text: '只有管理员可以执行更新' })
            return
        }
        setPreparingUpdate(true)
        setUpdateMsg(null)
        try {
            const res = await panelUpdateAPI.prepare()
            setUpdateInfo(res.data)
            setUpdateMsg({ type: 'success', text: '更新包已准备完成，请确认是否现在重启面板' })
        } catch (err) {
            setUpdateMsg({ type: 'error', text: err.response?.data?.error || '准备更新失败' })
        } finally {
            setPreparingUpdate(false)
        }
    }

    const handleRestartUpdate = async () => {
        if (!canManageUpdate) {
            setUpdateMsg({ type: 'error', text: '只有管理员可以执行更新' })
            return
        }
        setRestartingUpdate(true)
        setUpdateMsg(null)
        try {
            await panelUpdateAPI.restart()
            setUpdateMsg({ type: 'success', text: '面板正在重启，请稍后刷新页面' })
        } catch (err) {
            setUpdateMsg({ type: 'error', text: err.response?.data?.error || '重启失败' })
        } finally {
            setRestartingUpdate(false)
        }
    }

    const handleProfileSave = async () => {
        if (!profileForm.username.trim()) {
            setProfileMsg({ type: 'error', text: t('auth.username_required', '请输入用户名') })
            return
        }
        if (profileForm.new_password && profileForm.new_password !== profileForm.confirm_password) {
            setProfileMsg({ type: 'error', text: t('settings.password_mismatch', '两次密码输入不一致') })
            return
        }
        if (profileForm.new_password && profileForm.new_password.length < 8) {
            setProfileMsg({ type: 'error', text: t('settings.password_too_short', '新密码至少 8 个字符') })
            return
        }
        if (profileForm.new_password && !profileForm.current_password) {
            setProfileMsg({ type: 'error', text: t('settings.current_password_required', '请输入当前密码') })
            return
        }

        setSavingPassword(true)
        setProfileMsg(null)
        try {
            const res = await authAPI.updateProfile({
                username: profileForm.username,
                current_password: profileForm.current_password,
                new_password: profileForm.new_password,
            })
            setToken(res.data.token)
            setUser(res.data.user)
            setProfileForm((prev) => ({
                ...prev,
                username: res.data.user.username,
                current_password: '',
                new_password: '',
                confirm_password: '',
            }))
            setProfileMsg({ type: 'success', text: t('settings.admin_saved', '资料已更新') })
            setChangePasswordOpen(false)
        } catch (err) {
            setProfileMsg({ type: 'error', text: err.response?.data?.error || t('settings.save_failed', '保存失败') })
        } finally {
            setSavingPassword(false)
        }
    }

    const renderUpdateBadge = () => {
        if (!updateInfo?.update_available) return null
        return (
            <button
                type="button"
                onClick={() => handleUpdateDialogChange(true)}
                style={{ border: 0, background: 'transparent', padding: 0, cursor: 'pointer', lineHeight: 1 }}
                aria-label="查看面板新版本"
            >
                <Badge color="orange" variant="soft" size="1">
                    有新版本
                </Badge>
            </button>
        )
    }

    const sidebarContent = (
        <>
            <Flex align="center" gap="2" p="4" pb="2">
                <img src={logoImg} alt="PDai.TOP" style={{ width: 32, height: 32, borderRadius: 8 }} />
                <Text size="4" weight="bold" style={{ color: 'var(--cp-text)' }}>
                    派达面板
                </Text>
                {renderUpdateBadge()}
                {isMobile && (
                    <button
                        className="sidebar-btn"
                        style={{ marginLeft: 'auto', padding: 4, width: 'auto' }}
                        onClick={() => setSidebarOpen(false)}
                        aria-label={t('mobile.close_menu')}
                    >
                        <X size={18} />
                    </button>
                )}
            </Flex>

            <Separator size="4" style={{ background: 'var(--cp-border)' }} />

            <Box style={{ flex: 1, padding: '8px 12px', overflowY: 'auto' }}>
                <Flex direction="column" gap="1" mt="2">
                    {navItems.map((item) => (
                        <SidebarLink
                            key={item.to}
                            to={item.to}
                            icon={item.icon}
                            label={t(item.labelKey, item.labelFallback || item.labelKey)}
                            end={item.end}
                            onClick={handleNavClick}
                        />
                    ))}

                    {pluginNavItems.map((item) => {
                        const Icon = pluginIcons[item.icon] || BoxIcon
                        const label = t(`plugins.names.${item.pluginId}`, {
                            defaultValue: currentLang === 'zh' ? (item.labelZh || item.label) : (item.label || item.labelZh),
                        })
                        return (
                            <SidebarLink
                                key={`${item.pluginId}:${item.to}`}
                                to={item.to}
                                icon={Icon}
                                label={label}
                                onClick={handleNavClick}
                            />
                        )
                    })}

                    <Separator size="4" my="1" style={{ background: 'var(--cp-border)', opacity: 0.5 }} />
                    {bottomNavItems.map((item) => (
                        <SidebarLink
                            key={item.to}
                            to={item.to}
                            icon={item.icon}
                            label={t(item.labelKey, item.labelFallback || item.labelKey)}
                            end={item.end}
                            onClick={handleNavClick}
                        />
                    ))}
                </Flex>
            </Box>

            <Box p="3" style={{ borderTop: '1px solid var(--cp-border)' }}>
                <button
                    onClick={toggleLang}
                    className="sidebar-btn"
                    style={{ marginBottom: 4 }}
                >
                    <Languages size={16} />
                    <span>{currentLang === 'zh' ? 'EN' : '中文'}</span>
                </button>

                <DropdownMenu.Root>
                    <DropdownMenu.Trigger asChild>
                        <button className="sidebar-btn" style={{ marginBottom: 4 }}>
                            {themeMode === 'system' ? <Monitor size={16} /> : (theme === 'dark' ? <Moon size={16} /> : <Sun size={16} />)}
                            <span style={{ flex: 1, textAlign: 'left' }}>
                                {themeMode === 'system'
                                    ? t('nav.system_mode')
                                    : (theme === 'dark' ? t('nav.dark_mode') : t('nav.light_mode'))}
                            </span>
                            <ChevronDown size={14} />
                        </button>
                    </DropdownMenu.Trigger>
                    <DropdownMenu.Content side="top" align="start">
                        <DropdownMenu.RadioGroup value={themeMode} onValueChange={setThemeMode}>
                            <DropdownMenu.RadioItem value="system">
                                <Monitor size={14} /> {t('nav.system_mode')}
                            </DropdownMenu.RadioItem>
                            <DropdownMenu.RadioItem value="light">
                                <Sun size={14} /> {t('nav.light_mode')}
                            </DropdownMenu.RadioItem>
                            <DropdownMenu.RadioItem value="dark">
                                <Moon size={14} /> {t('nav.dark_mode')}
                            </DropdownMenu.RadioItem>
                        </DropdownMenu.RadioGroup>
                    </DropdownMenu.Content>
                </DropdownMenu.Root>

                <Dialog.Root open={changePasswordOpen} onOpenChange={setChangePasswordOpen}>
                    <DropdownMenu.Root>
                        <DropdownMenu.Trigger asChild>
                            <button className="sidebar-btn">
                                <User size={16} />
                                <span style={{ flex: 1, textAlign: 'left' }}>
                                    {user?.username || 'Admin'}
                                </span>
                                <ChevronDown size={14} />
                            </button>
                        </DropdownMenu.Trigger>
                        <DropdownMenu.Content side="top" align="start">
                            <DropdownMenu.Item onSelect={() => setChangePasswordOpen(true)}>
                                <KeyRound size={14} />
                                {t('settings.change_password', '修改密码')}
                            </DropdownMenu.Item>
                            <DropdownMenu.Item color="red" onClick={handleLogout}>
                                <LogOut size={14} />
                                {t('nav.sign_out')}
                            </DropdownMenu.Item>
                        </DropdownMenu.Content>
                    </DropdownMenu.Root>
                    <Dialog.Content maxWidth="420px">
                        <Dialog.Title>{t('settings.change_password', '修改密码')}</Dialog.Title>
                        <Dialog.Description size="2" color="gray">
                            {t('settings.change_password_hint', '修改当前登录用户名和密码。仅修改用户名时可不填写密码。')}
                        </Dialog.Description>
                        {profileMsg && (
                            <Callout.Root color={profileMsg.type === 'success' ? 'green' : 'red'} size="1" mt="3">
                                <Callout.Icon>
                                    {profileMsg.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                                </Callout.Icon>
                                <Callout.Text>{profileMsg.text}</Callout.Text>
                            </Callout.Root>
                        )}
                        <Flex direction="column" gap="3" mt="4">
                            <Box>
                                <Text size="2" weight="medium">{t('auth.username', '用户名')}</Text>
                                <TextField.Root value={profileForm.username} onChange={(e) => setProfileForm({ ...profileForm, username: e.target.value })} mt="1" />
                            </Box>
                            <Box>
                                <Text size="2" weight="medium">{t('settings.current_password', '当前密码')}</Text>
                                <TextField.Root type="password" value={profileForm.current_password} onChange={(e) => setProfileForm({ ...profileForm, current_password: e.target.value })} mt="1" />
                            </Box>
                            <Box>
                                <Text size="2" weight="medium">{t('settings.new_password', '新密码')}</Text>
                                <TextField.Root type="password" value={profileForm.new_password} onChange={(e) => setProfileForm({ ...profileForm, new_password: e.target.value })} mt="1" />
                            </Box>
                            <Box>
                                <Text size="2" weight="medium">{t('settings.confirm_password', '确认新密码')}</Text>
                                <TextField.Root type="password" value={profileForm.confirm_password} onChange={(e) => setProfileForm({ ...profileForm, confirm_password: e.target.value })} mt="1" />
                            </Box>
                            <Flex justify="end" gap="2" mt="2">
                                <Dialog.Close>
                                    <Button variant="soft">{t('common.cancel', '取消')}</Button>
                                </Dialog.Close>
                                <Button onClick={handleProfileSave} disabled={savingPassword || !profileForm.username.trim()}>
                                    {savingPassword && <Spinner size="1" />} {t('common.save', '保存')}
                                </Button>
                            </Flex>
                        </Flex>
                    </Dialog.Content>
                </Dialog.Root>
            </Box>
        </>
    )

    return (
        <Flex style={{ minHeight: '100vh' }}>
            {isMobile && (
                <Box className="mobile-topbar">
                    <button
                        className="hamburger-btn"
                        onClick={() => setSidebarOpen(true)}
                        aria-label={t('mobile.open_menu')}
                    >
                        <Menu size={22} />
                    </button>
                    <Flex align="center" gap="2">
                        <img src={logoImg} alt="PDai.TOP" style={{ width: 24, height: 24, borderRadius: 6 }} />
                        <Text size="3" weight="bold" style={{ color: 'var(--cp-text)' }}>
                            派达面板
                        </Text>
                        {renderUpdateBadge()}
                    </Flex>
                    <Box style={{ width: 22 }} />
                </Box>
            )}

            {isMobile && sidebarOpen && (
                <Box
                    className="sidebar-backdrop"
                    onClick={() => setSidebarOpen(false)}
                />
            )}

            <Box
                className={isMobile ? `sidebar-mobile ${sidebarOpen ? 'sidebar-mobile-open' : ''}` : ''}
                style={!isMobile ? {
                    width: 220,
                    minWidth: 220,
                    background: 'var(--cp-sidebar)',
                    borderRight: '1px solid var(--cp-border)',
                    display: 'flex',
                    flexDirection: 'column',
                } : undefined}
            >
                {sidebarContent}
            </Box>

            <Box
                style={{
                    flex: 1,
                    background: 'var(--cp-bg)',
                    overflow: 'auto',
                    position: 'relative',
                    ...(isMobile ? { paddingTop: 56 } : {}),
                }}
            >
                <Box p="5" style={{ maxWidth: 1200, margin: '0 auto', paddingBottom: 48, ...(isMobile ? { padding: '16px' } : {}) }}>
                    <Outlet />
                </Box>
                {version && (
                    <Text
                        size="1"
                        style={{
                            position: 'fixed',
                            bottom: 8,
                            right: 12,
                            color: 'var(--cp-text-muted)',
                            userSelect: 'none',
                            fontFamily: 'monospace',
                            fontSize: '0.7rem',
                        }}
                    >
                        PDai.TOP v{version}
                    </Text>
                )}
                {aiEnabled && <AIChatWidget />}
            </Box>

            <Dialog.Root open={updateDialogOpen} onOpenChange={handleUpdateDialogChange}>
                <Dialog.Content maxWidth="560px">
                    <Dialog.Title>
                        {updateInfo?.latest_version ? `发现新版本 v${updateInfo.latest_version}` : '面板更新'}
                    </Dialog.Title>
                    <Dialog.Description size="2" color="gray">
                        当前版本：{updateInfo?.current_version || version || '-'}；最新版本：{updateInfo?.tag_name || (updateInfo?.latest_version ? `v${updateInfo.latest_version}` : '-')}
                    </Dialog.Description>

                    {checkingUpdate && (
                        <Flex align="center" gap="2" mt="3">
                            <Spinner size="1" />
                            <Text size="2" color="gray">正在检查更新...</Text>
                        </Flex>
                    )}

                    {updateInfo?.published_at && (
                        <Text as="div" size="2" color="gray" mt="3">
                            发布时间：{new Date(updateInfo.published_at).toLocaleString()}
                        </Text>
                    )}

                    <Box
                        mt="3"
                        p="3"
                        style={{
                            maxHeight: 240,
                            overflowY: 'auto',
                            border: '1px solid var(--cp-border)',
                            borderRadius: 8,
                            background: 'var(--cp-surface)',
                        }}
                    >
                        {updateInfo?.body ? (
                            <Markdown components={releaseMarkdownComponents}>{updateInfo.body}</Markdown>
                        ) : (
                            <Text as="div" size="2" color="gray">暂无更新说明</Text>
                        )}
                    </Box>

                    {updateInfo?.html_url && (
                        <Button asChild size="2" variant="soft" mt="3">
                            <a href={updateInfo.html_url} target="_blank" rel="noreferrer">
                                查看 GitHub Release
                            </a>
                        </Button>
                    )}

                    {!canManageUpdate && (
                        <Callout.Root color="amber" size="1" mt="3">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>当前账号可以查看更新；准备更新和重启面板仅限管理员或所有者。</Callout.Text>
                        </Callout.Root>
                    )}

                    {updateInfo?.reason && (
                        <Callout.Root color="amber" size="1" mt="3">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>{updateInfo.reason}</Callout.Text>
                        </Callout.Root>
                    )}

                    {updateInfo?.prepared && (
                        <Callout.Root color="green" size="1" mt="3">
                            <Callout.Icon><CheckCircle2 size={14} /></Callout.Icon>
                            <Callout.Text>更新包已准备完成，是否现在重启面板？重启时会启动临时 helper 进程延迟替换并重新拉起面板。</Callout.Text>
                        </Callout.Root>
                    )}

                    {updateMsg && (
                        <Callout.Root color={updateMsg.type === 'success' ? 'green' : 'red'} size="1" mt="3">
                            <Callout.Icon>
                                {updateMsg.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                            </Callout.Icon>
                            <Callout.Text>{updateMsg.text}</Callout.Text>
                        </Callout.Root>
                    )}

                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close>
                            <Button variant="soft" disabled={preparingUpdate || restartingUpdate}>
                                稍后
                            </Button>
                        </Dialog.Close>
                        {updateInfo?.prepared ? (
                            <Button color="red" onClick={handleRestartUpdate} disabled={!canManageUpdate || restartingUpdate}>
                                {restartingUpdate && <Spinner size="1" />} 现在重启面板
                            </Button>
                        ) : (
                            <Button onClick={handlePrepareUpdate} disabled={!canManageUpdate || preparingUpdate || checkingUpdate || !updateInfo?.can_update}>
                                {preparingUpdate ? <Spinner size="1" /> : <DownloadCloud size={16} />} 立即更新
                            </Button>
                        )}
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Flex>
    )
}
