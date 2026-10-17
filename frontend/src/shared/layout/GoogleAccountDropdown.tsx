import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Cloud,
  RefreshCw,
  LogOut,
  FolderSync,
  DownloadCloud,
  Sliders,
  ExternalLink,
  ChevronDown,
} from 'lucide-react'
import { Button, Modal, toast } from '../components'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  defaultGDriveSettings,
  fetchGDriveSettings,
  revokeGDriveToken,
  startGDriveOAuth,
  syncGDriveNow,
  restoreLatestGDriveBackup,
  toggleGDriveAutoSync,
  type GDriveConnection,
} from '../../modules/backup/channels/gdrive/api'

export function GoogleLogo({ className = 'w-4 h-4' }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24">
      <path
        fill="#4285F4"
        d="M23.745 12.27c0-.7-.06-1.4-.19-2.07H12v4.51h6.6c-.29 1.52-1.14 2.8-2.4 3.65v3.03h3.88c2.27-2.09 3.665-5.17 3.665-9.12z"
      />
      <path
        fill="#34A853"
        d="M12 24c3.24 0 5.95-1.08 7.93-2.91l-3.88-3.03c-1.08.72-2.45 1.16-4.05 1.16-3.12 0-5.77-2.1-6.72-4.93H1.26v3.13C3.27 21.36 7.35 24 12 24z"
      />
      <path
        fill="#FBBC05"
        d="M5.28 14.29c-.25-.72-.38-1.49-.38-2.29s.13-1.57.38-2.29V6.58H1.26C.46 8.17 0 9.97 0 12s.46 3.83 1.26 5.42l4.02-3.13z"
      />
      <path
        fill="#EA4335"
        d="M12 4.75c1.77 0 3.35.61 4.6 1.8l3.42-3.42C17.95 1.19 15.24 0 12 0 7.35 0 3.27 2.64 1.26 6.58l4.02 3.13c.95-2.83 3.6-4.96 6.72-4.96z"
      />
    </svg>
  )
}

export function GoogleAccountDropdown() {
  const [connection, setConnection] = useState<GDriveConnection>(defaultGDriveSettings)
  const [loading, setLoading] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const [authorizing, setAuthorizing] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const [restoreConfirmOpen, setRestoreConfirmOpen] = useState(false)
  const [revokeConfirmOpen, setRevokeConfirmOpen] = useState(false)

  const dropdownRef = useRef<HTMLDivElement>(null)

  const reloadSettings = async () => {
    try {
      const next = await fetchGDriveSettings()
      setConnection(next)
    } catch {
      // ignore
    }
  }

  useEffect(() => {
    void reloadSettings()

    // 监听各类云端事件
    const cleanups: Array<() => void> = []

    cleanups.push(
      EventsOn('backup:gdrive:oauth_completed', (data: any) => {
        setAuthorizing(false)
        if (data?.ok) {
          toast.success(`Google 账号登录成功！${data.email ? `（${data.email}）` : ''}`)
          void reloadSettings()
        } else {
          toast.error(data?.error || 'Google 账号授权未完成')
        }
      })
    )

    cleanups.push(
      EventsOn('backup:gdrive:synced', (data: any) => {
        setSyncing(false)
        if (data?.lastSyncTime) {
          setConnection((prev) => ({ ...prev, lastSyncTime: data.lastSyncTime }))
        }
      })
    )

    cleanups.push(
      EventsOn('backup:gdrive:sync_started', () => {
        setSyncing(true)
      })
    )

    cleanups.push(
      EventsOn('backup:gdrive:sync_failed', (data: any) => {
        setSyncing(false)
        if (data?.isAutoSync) {
          // 后台静默同步失败不弹出全局 toast.error，避免打扰正常操作并污染通知中心
          console.warn('Google Drive 后台自动同步未完成:', data?.error)
          return
        }
        toast.error(`云端自动同步失败: ${data?.error || '未知错误'}`)
      })
    )

    cleanups.push(
      EventsOn('backup:gdrive:revoked', () => {
        void reloadSettings()
      })
    )

    return () => {
      cleanups.forEach((c) => c())
    }
  }, [])

  // 点击外部关闭下拉菜单 & 展开时重新拉取最新同步状态
  useEffect(() => {
    if (menuOpen) {
      void reloadSettings()
    }
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setMenuOpen(false)
      }
    }
    if (menuOpen) {
      document.addEventListener('mousedown', handleClickOutside)
    }
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [menuOpen])

  const handleStartOAuth = async () => {
    setAuthorizing(true)
    try {
      await startGDriveOAuth()
      toast.info('已唤起系统浏览器打开 Google 登录授权页面，请在浏览器中完成登录')
    } catch (err: any) {
      setAuthorizing(false)
      const errorMsg = err?.message || (typeof err === 'string' ? err : '发起 Google 登录失败')
      toast.error(errorMsg)
    }
  }

  const handleManualSync = async () => {
    setSyncing(true)
    try {
      const res = await syncGDriveNow()
      toast.success('配置已成功同步到 Google Drive！')
      if (res?.lastSyncTime) {
        setConnection((prev) => ({ ...prev, lastSyncTime: res.lastSyncTime }))
      }
    } catch (err: any) {
      toast.error(err?.message || '同步配置失败')
    } finally {
      setSyncing(false)
    }
  }

  const handleRestoreLatest = async () => {
    setRestoring(true)
    setRestoreConfirmOpen(false)
    try {
      const res = await restoreLatestGDriveBackup()
      toast.success(`云端配置恢复完成！(${res.fileName || '最新备份'})`)
      setMenuOpen(false)
      // 触发页面刷新或通知
      window.location.reload()
    } catch (err: any) {
      toast.error(err?.message || '恢复云端配置失败')
    } finally {
      setRestoring(false)
    }
  }

  const handleToggleAutoSync = async () => {
    const nextVal = !connection.autoSync
    try {
      const updated = await toggleGDriveAutoSync(nextVal)
      setConnection(updated)
      toast.success(nextVal ? '已开启配置自动同步' : '已关闭配置自动同步')
    } catch (err: any) {
      toast.error(err?.message || '更新自动同步设置失败')
    }
  }

  const handleRevoke = async () => {
    setLoading(true)
    setRevokeConfirmOpen(false)
    try {
      await revokeGDriveToken()
      toast.success('已退出 Google 账号并断开云端同步')
      setMenuOpen(false)
      void reloadSettings()
    } catch (err: any) {
      toast.error(err?.message || '退出登录失败')
    } finally {
      setLoading(false)
    }
  }

  const isConfigured = connection.configured && Boolean(connection.accountEmail)
  const displayName =
    connection.accountName ||
    (connection.accountEmail ? connection.accountEmail.split('@')[0] : 'Google 账号')
  const initialLetter = (connection.accountName || connection.accountEmail || 'G')
    .slice(0, 1)
    .toUpperCase()

  return (
    <div className="relative" ref={dropdownRef}>
      {/* 顶部主按钮 */}
      {isConfigured ? (
        <button
          onClick={() => setMenuOpen(!menuOpen)}
          className={`flex items-center gap-2 pl-1 pr-2 py-1 rounded-full border transition-all duration-150 ${
            menuOpen
              ? 'bg-[var(--color-accent-muted)] border-[var(--color-accent)] shadow-sm'
              : 'border-[var(--color-border-default)] hover:border-[var(--color-border-strong)] hover:bg-[var(--color-bg-muted)]'
          }`}
          title={`Google 账号: ${connection.accountEmail}${
            connection.lastSyncTime ? `\n上次同步时间: ${connection.lastSyncTime}` : '\n暂未同步配置'
          } (点击查看同步详情)`}
        >
          {connection.accountAvatar ? (
            <img
              src={connection.accountAvatar}
              alt={displayName}
              className="w-6 h-6 rounded-full object-cover shadow-xs"
              onError={(e) => {
                // 回退到文字头像
                ;(e.currentTarget as HTMLElement).style.display = 'none'
              }}
            />
          ) : (
            <div className="w-6 h-6 rounded-full bg-gradient-to-tr from-blue-600 to-indigo-500 text-white font-semibold text-xs flex items-center justify-center shadow-xs">
              {initialLetter}
            </div>
          )}

          <div className="flex flex-col text-left max-w-[120px]">
            <span className="text-xs font-medium text-[var(--color-text-primary)] truncate leading-tight">
              {displayName}
            </span>
          </div>

          {syncing ? (
            <RefreshCw className="w-3 h-3 text-blue-500 animate-spin shrink-0" />
          ) : (
            <span
              className="w-1.5 h-1.5 rounded-full bg-emerald-500 shrink-0"
              title="云端同步已连接"
            />
          )}

          <ChevronDown
            className={`w-3 h-3 text-[var(--color-text-muted)] transition-transform duration-150 ${
              menuOpen ? 'rotate-180' : ''
            }`}
          />
        </button>
      ) : (
        <button
          onClick={handleStartOAuth}
          disabled={authorizing}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-full border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:border-[var(--color-accent)] hover:bg-[var(--color-accent-muted)] transition-all shadow-xs disabled:opacity-60 cursor-pointer"
          title="点击直接使用 Google 账号登录并开启云端同步"
        >
          {authorizing ? (
            <>
              <RefreshCw className="w-3.5 h-3.5 text-blue-500 animate-spin" />
              <span>正在打开浏览器授权...</span>
            </>
          ) : (
            <>
              <GoogleLogo className="w-3.5 h-3.5" />
              <span>登录 Google 账号</span>
            </>
          )}
        </button>
      )}

      {/* 已登录下拉气泡面板 */}
      {menuOpen && isConfigured && (
        <div className="absolute right-0 mt-2 w-80 rounded-2xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] shadow-xl p-3 z-50 animate-fade-in text-sm text-[var(--color-text-primary)]">
          {/* 用户信息头部 */}
          <div className="flex items-center gap-3 p-2 bg-[var(--color-bg-muted)] rounded-xl mb-3">
            {connection.accountAvatar ? (
              <img
                src={connection.accountAvatar}
                alt={displayName}
                className="w-10 h-10 rounded-full object-cover border border-[var(--color-border-default)] shadow-xs"
              />
            ) : (
              <div className="w-10 h-10 rounded-full bg-gradient-to-tr from-blue-600 to-indigo-500 text-white font-bold text-base flex items-center justify-center shadow-xs">
                {initialLetter}
              </div>
            )}
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <span className="font-semibold text-sm truncate">{displayName}</span>
                <span className="inline-flex items-center px-1.5 py-0.2 text-[10px] font-medium rounded-md bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
                  已连接
                </span>
              </div>
              <p className="text-xs text-[var(--color-text-muted)] truncate">
                {connection.accountEmail}
              </p>
            </div>
          </div>

          {/* 自动同步设置卡片 */}
          <div className="p-2.5 rounded-xl border border-[var(--color-border-default)] mb-2 space-y-2 bg-[var(--color-bg-muted)]/40">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-1.5 text-xs font-medium">
                <FolderSync className="w-4 h-4 text-[var(--color-accent)]" />
                <span>自动同步配置</span>
              </div>
              <label className="relative inline-flex items-center cursor-pointer">
                <input
                  type="checkbox"
                  checked={connection.autoSync}
                  onChange={handleToggleAutoSync}
                  className="sr-only peer"
                />
                <div className="w-8 h-4 bg-gray-300 peer-focus:outline-hidden rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[1px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-3.5 after:w-3.5 after:transition-all peer-checked:bg-[var(--color-accent)]"></div>
              </label>
            </div>

            <div className="text-[11px] text-[var(--color-text-muted)] leading-tight space-y-1">
              <div className="flex items-center justify-between">
                <span>云端目录:</span>
                <span className="text-[var(--color-text-secondary)] font-mono truncate max-w-[170px]" title={connection.folderName || 'AntBrowserBackup'}>
                  {connection.folderName || 'AntBrowserBackup'}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span>最近同步时间:</span>
                <span className={`font-mono text-[11px] ${connection.lastSyncTime ? 'text-emerald-600 dark:text-emerald-400 font-medium' : 'text-[var(--color-text-muted)]'}`}>
                  {connection.lastSyncTime || '暂未同步'}
                </span>
              </div>
            </div>
          </div>

          {/* 快捷操作列表 */}
          <div className="space-y-1">
            <button
              onClick={handleManualSync}
              disabled={syncing}
              className="w-full flex items-center justify-between px-2.5 py-2.5 text-xs rounded-lg hover:bg-[var(--color-bg-muted)] transition-colors disabled:opacity-50 text-left font-medium group"
            >
              <div className="flex items-center gap-2.5 min-w-0">
                <RefreshCw
                  className={`w-4 h-4 text-blue-500 shrink-0 ${syncing ? 'animate-spin' : ''}`}
                />
                <div className="flex flex-col min-w-0 text-left">
                  <span className="text-xs font-medium text-[var(--color-text-primary)]">
                    立即同步到云端
                  </span>
                  <span className="text-[10px] text-[var(--color-text-muted)] font-normal truncate mt-0.5">
                    {syncing
                      ? '正在同步上传配置...'
                      : connection.lastSyncTime
                      ? `同步时间: ${connection.lastSyncTime}`
                      : '暂无同步记录'}
                  </span>
                </div>
              </div>
              <span className="text-[10px] text-[var(--color-text-muted)] shrink-0 ml-2 group-hover:text-[var(--color-accent)] font-normal transition-colors">
                {syncing ? '正在同步...' : '备份当前配置'}
              </span>
            </button>

            <button
              onClick={() => setRestoreConfirmOpen(true)}
              disabled={restoring}
              className="w-full flex items-center justify-between px-2.5 py-2.5 text-xs rounded-lg hover:bg-[var(--color-bg-muted)] transition-colors disabled:opacity-50 text-left font-medium group"
            >
              <div className="flex items-center gap-2.5 min-w-0">
                <DownloadCloud className="w-4 h-4 text-emerald-500 shrink-0" />
                <div className="flex flex-col min-w-0 text-left">
                  <span className="text-xs font-medium text-[var(--color-text-primary)]">
                    从云端恢复配置
                  </span>
                  <span className="text-[10px] text-[var(--color-text-muted)] font-normal truncate mt-0.5">
                    {restoring
                      ? '正在从云端拉取配置...'
                      : connection.lastSyncTime
                      ? `最新备份: ${connection.lastSyncTime}`
                      : '拉取云端最新备份'}
                  </span>
                </div>
              </div>
              <span className="text-[10px] text-[var(--color-text-muted)] shrink-0 ml-2 group-hover:text-emerald-500 font-normal transition-colors">
                {restoring ? '正在恢复...' : '拉取最新备份'}
              </span>
            </button>

            <Link
              to="/backup"
              onClick={() => setMenuOpen(false)}
              className="w-full flex items-center justify-between px-2.5 py-2 text-xs rounded-lg hover:bg-[var(--color-bg-muted)] transition-colors text-left font-medium"
            >
              <span className="flex items-center gap-2">
                <Sliders className="w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                <span>备份与同步管理</span>
              </span>
              <ExternalLink className="w-3 h-3 text-[var(--color-text-muted)]" />
            </Link>

            <Link
              to="/profile"
              onClick={() => setMenuOpen(false)}
              className="w-full flex items-center justify-between px-2.5 py-2 text-xs rounded-lg hover:bg-[var(--color-bg-muted)] transition-colors text-left font-medium"
            >
              <span className="flex items-center gap-2">
                <Cloud className="w-3.5 h-3.5 text-[var(--color-text-muted)]" />
                <span>关于大猫科技</span>
              </span>
            </Link>
          </div>

          <div className="border-t border-[var(--color-border-default)] mt-2 pt-2">
            <button
              onClick={() => setRevokeConfirmOpen(true)}
              className="w-full flex items-center gap-2 px-2.5 py-1.5 text-xs text-red-500 hover:bg-red-500/10 rounded-lg transition-colors font-medium text-left"
            >
              <LogOut className="w-3.5 h-3.5" />
              <span>退出 Google 账号</span>
            </button>
          </div>
        </div>
      )}



      {/* 从云端恢复确认弹窗 */}
      <Modal
        open={restoreConfirmOpen}
        onClose={() => setRestoreConfirmOpen(false)}
        title="确认从 Google Drive 恢复最新配置？"
        width="440px"
        footer={
          <div className="flex justify-end gap-2 w-full">
            <Button variant="secondary" onClick={() => setRestoreConfirmOpen(false)}>
              取消
            </Button>
            <Button onClick={handleRestoreLatest} loading={restoring}>
              开始恢复
            </Button>
          </div>
        }
      >
        <p className="text-sm text-[var(--color-text-secondary)] py-2">
          将自动从您的 Google Drive 云端下载最新的配置备份包并合并到本地数据库（包含环境配置、代理、指纹、分组等）。是否继续？
        </p>
      </Modal>

      {/* 退出账号确认弹窗 */}
      <Modal
        open={revokeConfirmOpen}
        onClose={() => setRevokeConfirmOpen(false)}
        title="退出 Google 账号"
        width="440px"
        footer={
          <div className="flex justify-end gap-2 w-full">
            <Button variant="secondary" onClick={() => setRevokeConfirmOpen(false)}>
              取消
            </Button>
            <Button variant="danger" onClick={handleRevoke} loading={loading}>
              退出登录
            </Button>
          </div>
        }
      >
        <p className="text-sm text-[var(--color-text-secondary)] py-2">
          退出后将断开 Google Drive 连接，并停止云端自动同步配置。本地已有环境不受影响。是否确认退出？
        </p>
      </Modal>
    </div>
  )
}
