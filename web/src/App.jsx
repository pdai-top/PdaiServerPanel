import { BrowserRouter, Routes, Route, Navigate } from 'react-router'
import { useEffect } from 'react'
import { useAuthStore } from './stores/auth.js'
import { settingAPI } from './api/index.js'
import { applyPageTitle } from './utils/pageTitle.js'
import Login from './pages/Login.jsx'
import Layout from './pages/Layout.jsx'
import Dashboard from './pages/Dashboard.jsx'
import HostList from './pages/HostList.jsx'
import Settings from './pages/Settings.jsx'
import DockerOverview from './pages/DockerOverview.jsx'
import DockerImages from './pages/DockerImages.jsx'
import DockerSettings from './pages/DockerSettings.jsx'
import FileManager from './pages/FileManager.jsx'
import FileEditor from './pages/FileEditor.jsx'
import WebTerminal from './pages/WebTerminal.jsx'
import DatabaseInstances from './pages/DatabaseInstances.jsx'
import DatabaseDetail from './pages/DatabaseDetail.jsx'
import DatabaseQuery from './pages/DatabaseQuery.jsx'
import SQLiteBrowser from './pages/SQLiteBrowser.jsx'
import MonitoringDashboard from './pages/MonitoringDashboard.jsx'
import FirewallManager from './pages/FirewallManager.jsx'
import CronJobManager from './pages/CronJobManager.jsx'
import SupervisorManager from './pages/SupervisorManager.jsx'
import PluginsPage from './pages/PluginsPage.jsx'
import AppStore from './pages/AppStore.jsx'
import AppDetail from './pages/AppDetail.jsx'
import TemplateMarket from './pages/TemplateMarket.jsx'

function ProtectedRoute({ children }) {
    const token = useAuthStore((s) => s.token)
    if (!token) return <Navigate to="/login" replace />
    return children
}

export default function App() {
    const { token, checkSetup, fetchMe } = useAuthStore()

    useEffect(() => {
        checkSetup()
        if (token) fetchMe()
    }, [])

    useEffect(() => {
        if (!token) {
            applyPageTitle('')
            return
        }
        settingAPI.getAll().then((res) => {
            applyPageTitle(res.data?.settings?.site_name || '')
        }).catch(() => {
            applyPageTitle('')
        })
    }, [token])

    return (
        <BrowserRouter>
            <Routes>
                <Route path="/login" element={<Login />} />
                <Route
                    path="/"
                    element={
                        <ProtectedRoute>
                            <Layout />
                        </ProtectedRoute>
                    }
                >
                    <Route index element={<Dashboard />} />
                    <Route path="hosts" element={<HostList />} />
                    <Route path="editor" element={<Navigate to="/settings?tab=caddyfile" replace />} />
                    <Route path="docker" element={<DockerOverview />} />
                    <Route path="docker/images" element={<DockerImages />} />
                    <Route path="docker/settings" element={<DockerSettings />} />
                    <Route path="files" element={<FileManager />} />
                    <Route path="files/edit" element={<FileEditor />} />
                    <Route path="terminal" element={<WebTerminal />} />
                    <Route path="database" element={<DatabaseInstances />} />
                    <Route path="database/sqlite" element={<SQLiteBrowser />} />
                    <Route path="database/query" element={<DatabaseQuery />} />
                    <Route path="database/:id" element={<DatabaseDetail />} />
                    <Route path="settings" element={<Settings />} />
                    <Route path="monitoring" element={<MonitoringDashboard />} />
                    <Route path="firewall" element={<FirewallManager />} />
                    <Route path="cronjob" element={<CronJobManager />} />
                    <Route path="supervisor" element={<SupervisorManager />} />
                    <Route path="plugins" element={<PluginsPage />} />
                    <Route path="store" element={<AppStore />} />
                    <Route path="store/app/:id" element={<AppDetail />} />
                    <Route path="store/templates" element={<TemplateMarket />} />

                    <Route path="logs" element={<Navigate to="/settings" replace />} />
                    <Route path="dns" element={<Navigate to="/settings" replace />} />
                    <Route path="docker/containers" element={<Navigate to="/docker" replace />} />
                    <Route path="docker/networks" element={<Navigate to="/docker" replace />} />
                    <Route path="docker/volumes" element={<Navigate to="/docker" replace />} />
                </Route>
                <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
        </BrowserRouter>
    )
}
