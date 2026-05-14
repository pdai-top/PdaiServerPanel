import { Box, Flex, Text, DropdownMenu, Separator, Dialog, Button, TextField, Spinner, Callout } from '@radix-ui/themes'
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
} from 'lucide-react'
import { useAuthStore } from '../stores/auth.js'
import { useThemeStore } from '../stores/theme.js'
import { usePluginNavStore } from '../stores/pluginNav.js'
import { authAPI, dashboardAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import logoImg from '../assets/logo.png'

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
}

const bottomNavItems = [
    { to: '/plugins', icon: Puzzle, labelKey: 'nav.plugins', labelFallback: '插件管理' },
    { to: '/settings', icon: Settings, labelKey: 'nav.settings' },
]

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
    const { theme, toggle: toggleTheme } = useThemeStore()
    const pluginNavItems = usePluginNavStore((s) => s.navItems)
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

    const currentLang = i18n.language?.startsWith('zh') ? 'zh' : 'en'

    const toggleLang = () => {
        const next = currentLang === 'zh' ? 'en' : 'zh'
        i18n.changeLanguage(next)
    }

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

    const sidebarContent = (
        <>
            <Flex align="center" gap="2" p="4" pb="2">
                <img src={logoImg} alt="Pdai" style={{ width: 32, height: 32, borderRadius: 8 }} />
                <Text size="4" weight="bold" style={{ color: 'var(--cp-text)' }}>
                    Pdai
                </Text>
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
                        const label = currentLang === 'zh' ? (item.labelZh || item.label) : (item.label || item.labelZh)
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

                <button
                    onClick={toggleTheme}
                    className="sidebar-btn"
                    style={{ marginBottom: 4 }}
                >
                    {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
                    <span>{theme === 'dark' ? t('nav.light_mode') : t('nav.dark_mode')}</span>
                </button>

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
                        <img src={logoImg} alt="Pdai" style={{ width: 24, height: 24, borderRadius: 6 }} />
                        <Text size="3" weight="bold" style={{ color: 'var(--cp-text)' }}>
                            Pdai
                        </Text>
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
                        Pdai v{version}
                    </Text>
                )}
            </Box>
        </Flex>
    )
}