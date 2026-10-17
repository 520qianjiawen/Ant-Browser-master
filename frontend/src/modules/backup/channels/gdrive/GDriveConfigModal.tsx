import { useEffect, useState } from 'react'
import { CheckCircle2, Cloud, ExternalLink, KeyRound, Lock, RefreshCw, XCircle } from 'lucide-react'
import { Badge, Button, ConfirmModal, FormItem, Input, Modal, toast } from '../../../../shared/components'
import { BrowserOpenURL, EventsOn } from '../../../../wailsjs/runtime/runtime'
import {
  defaultGDriveSettings,
  fetchGDriveSettings,
  revokeGDriveToken,
  saveGDriveSettings,
  startGDriveOAuth,
  testGDriveConnection,
  type GDriveConnection,
  type GDriveDraft,
} from './api'

interface GDriveConfigModalProps {
  open: boolean
  onClose: () => void
  onConfigured?: (connection: GDriveConnection) => void
  onBusyChange?: (busy: boolean) => void
}

export function GDriveConfigModal({ open, onClose, onConfigured, onBusyChange }: GDriveConfigModalProps) {
  const [connection, setConnection] = useState<GDriveConnection>(defaultGDriveSettings)
  const [draft, setDraft] = useState<GDriveDraft>({
    clientID: '',
    clientSecret: '',
    folderName: defaultGDriveSettings.folderName,
    proxyURL: '',
  })
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [authorizing, setAuthorizing] = useState(false)
  const [revoking, setRevoking] = useState(false)

  useEffect(() => {
    onBusyChange?.(loading || saving || testing || authorizing || revoking)
  }, [loading, saving, testing, authorizing, revoking, onBusyChange])
  const [revokeConfirmOpen, setRevokeConfirmOpen] = useState(false)
  const [showHelp, setShowHelp] = useState(false)
  const [testResult, setTestResult] = useState<{ status: 'success' | 'error'; message: string } | null>(null)

  const reloadSettings = async () => {
    setLoading(true)
    try {
      const next = await fetchGDriveSettings()
      setConnection(next)
      if (!next.configured && !next.clientID) {
        setShowHelp(true)
      }
      setDraft((current) => ({
        ...current,
        clientID: next.clientID || current.clientID,
        folderName: next.folderName || current.folderName,
        proxyURL: next.proxyURL || current.proxyURL,
      }))
    } catch (err: any) {
      toast.error(err?.message || '读取 Google Drive 设置失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (open) {
      void reloadSettings()
    }
  }, [open])

  useEffect(() => {
    if (!open) return

    // 监听后端 OAuth 授权完成事件
    const cleanup = EventsOn('backup:gdrive:oauth_completed', (data: any) => {
      setAuthorizing(false)
      if (data?.ok) {
        toast.success(`Google Drive 授权成功！${data.email ? `（${data.email}）` : ''}`)
        void reloadSettings().then(() => {
          onConfigured?.(connection)
        })
      } else {
        toast.error(data?.error || 'Google Drive 授权未完成')
      }
    })

    return () => {
      cleanup()
    }
  }, [open])

  const handleStartOAuth = async () => {
    if (!draft.clientID.trim()) {
      setShowHelp(true)
      toast.warning('请先填写 Client ID（可参考下方 1 分钟快速获取指引）')
      return
    }
    if (!draft.clientSecret.trim() && !connection.clientSecretConfigured) {
      setShowHelp(true)
      toast.warning('请先填写 Client Secret')
      return
    }

    setAuthorizing(true)
    setTestResult(null)
    try {
      // 先保存一次当前填写的配置
      await saveGDriveSettings(draft)
      await startGDriveOAuth(draft)
      toast.info('已在浏览器中打开 Google 授权页面，请完成登录与授权')
    } catch (err: any) {
      setAuthorizing(false)
      toast.error(err?.message || '发起 Google 授权失败')
    }
  }

  const handleTest = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const res = await testGDriveConnection(draft)
      if (res.ok) {
        setTestResult({
          status: 'success',
          message: `${res.message} ${res.email ? `（${res.email}）` : ''}`,
        })
        toast.success('Google Drive 连接正常')
      } else {
        setTestResult({ status: 'error', message: res.message || '连接失败' })
      }
    } catch (err: any) {
      const msg = err?.message || '测试连接异常'
      setTestResult({ status: 'error', message: msg })
      toast.error(msg)
    } finally {
      setTesting(false)
    }
  }

  const handleRevokeConfirm = async () => {
    setRevoking(true)
    try {
      const next = await revokeGDriveToken()
      setConnection(next)
      setRevokeConfirmOpen(false)
      toast.success('已撤销 Google Drive 访问令牌')
      onConfigured?.(next)
    } catch (err: any) {
      toast.error(err?.message || '撤销令牌失败')
    } finally {
      setRevoking(false)
    }
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const next = await saveGDriveSettings(draft)
      setConnection(next)
      toast.success('Google Drive 配置已保存')
      onConfigured?.(next)
      onClose()
    } catch (err: any) {
      toast.error(err?.message || '保存配置失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <Modal
        open={open}
        onClose={onClose}
        title="Google Drive 备份设置"
        width="560px"
        footer={(
          <>
            <Button variant="secondary" onClick={onClose}>取消</Button>
            <Button onClick={handleSave} loading={saving || loading}>保存设置</Button>
          </>
        )}
      >
        <div className="space-y-4">
          {/* 云端授权状态卡片 */}
          <div className={`rounded-xl border p-4 transition-colors ${
            connection.configured
              ? 'border-[var(--color-success)]/30 bg-[var(--color-success)]/5'
              : 'border-[var(--color-border-default)] bg-[var(--color-bg-muted)]'
          }`}>
            <div className="flex items-start justify-between gap-3">
              <div className="flex items-center gap-2.5">
                <div className={`flex h-10 w-10 items-center justify-center rounded-xl ${
                  connection.configured
                    ? 'bg-[var(--color-success)]/10 text-[var(--color-success)]'
                    : 'bg-[var(--color-border-default)] text-[var(--color-text-muted)]'
                }`}>
                  <Cloud className="h-5 w-5" />
                </div>
                <div>
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-[var(--color-text-primary)]">Google 云端硬盘</span>
                    <Badge variant={connection.configured ? 'success' : 'default'} size="sm" dot>
                      {connection.configured ? '已授权连接' : '未连接'}
                    </Badge>
                  </div>
                  <div className="mt-0.5 text-xs text-[var(--color-text-muted)] space-y-0.5">
                    {connection.configured ? (
                      <>
                        <div>绑定账号：{connection.accountEmail || 'Google User'}</div>
                        {connection.lastSyncTime ? (
                          <div className="text-[11px] text-[var(--color-text-secondary)] font-mono">
                            最近同步时间：{connection.lastSyncTime}
                          </div>
                        ) : null}
                      </>
                    ) : (
                      '通过 OAuth 2.0 协议将备份直接存储到 Google Drive'
                    )}
                  </div>
                </div>
              </div>

              {connection.configured ? (
                <div className="flex items-center gap-1.5">
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    onClick={handleTest}
                    loading={testing}
                  >
                    <RefreshCw className="h-3.5 w-3.5" />
                    测试
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="danger"
                    onClick={() => setRevokeConfirmOpen(true)}
                    loading={revoking}
                  >
                    撤销访问令牌
                  </Button>
                </div>
              ) : null}
            </div>

            {!connection.configured ? (
              <div className="mt-3.5 border-t border-[var(--color-border-muted)] pt-3">
                <Button
                  type="button"
                  onClick={handleStartOAuth}
                  loading={authorizing}
                  className="w-full"
                >
                  <KeyRound className="h-4 w-4" />
                  {authorizing ? '正在等待浏览器授权...' : '授权连接 Google 账号'}
                </Button>
              </div>
            ) : null}
          </div>

          {/* 测试结果提示 */}
          {testResult ? (
            <div className={`flex items-center gap-2 rounded-lg border p-3 text-sm ${
              testResult.status === 'success'
                ? 'border-[var(--color-success)]/30 bg-[var(--color-success)]/10 text-[var(--color-success)]'
                : 'border-[var(--color-error)]/30 bg-[var(--color-error)]/10 text-[var(--color-error)]'
            }`}>
              {testResult.status === 'success' ? <CheckCircle2 className="h-4 w-4 shrink-0" /> : <XCircle className="h-4 w-4 shrink-0" />}
              <span>{testResult.message}</span>
            </div>
          ) : null}

          {/* 凭据配置区 */}
          <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4 space-y-3.5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-1.5 text-sm font-medium text-[var(--color-text-primary)]">
                <Lock className="h-4 w-4 text-[var(--color-text-muted)]" />
                Google OAuth 2.0 凭据
              </div>
              <button
                type="button"
                className="text-xs text-[var(--color-accent)] hover:underline flex items-center gap-1"
                onClick={() => setShowHelp(!showHelp)}
              >
                {showHelp ? '收起配置指引' : '如何获取凭据？'}
              </button>
            </div>

            {showHelp ? (
              <div className="rounded-lg bg-[var(--color-bg-muted)] p-3 text-xs leading-relaxed text-[var(--color-text-secondary)] space-y-2 border border-[var(--color-border-muted)]">
                <div className="flex items-center justify-between font-semibold text-[var(--color-text-primary)]">
                  <span>Google Cloud 凭据获取指引（约需 1 分钟）：</span>
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    className="!h-6 !px-2 text-xs !bg-[var(--color-bg-surface)] hover:!bg-[var(--color-border-default)]"
                    onClick={() => BrowserOpenURL('https://console.cloud.google.com/apis/credentials')}
                  >
                    <ExternalLink className="h-3 w-3 mr-1" />
                    打开控制台
                  </Button>
                </div>
                <div><strong>1. 创建项目：</strong>登录 <button type="button" className="text-[var(--color-accent)] underline inline-flex items-center" onClick={() => BrowserOpenURL('https://console.cloud.google.com/')}>Google Cloud Console</button> 并新建一个项目（例如 AntBrowser）。</div>
                <div><strong>2. 启用 API：</strong>前往【API 和服务】→【库】，搜索 <strong>Google Drive API</strong> 并点击「启用」。</div>
                <div><strong>3. 同意屏幕：</strong>前往【OAuth 同意屏幕】，用户类型选择「外部」，填入应用名称与您的邮箱，保存后在「测试用户」中添加您的 Google 邮箱。</div>
                <div><strong>4. 创建凭据：</strong>前往【凭据】页面，点击【创建凭据】→【OAuth 客户端 ID】，应用类型务必选择 <strong>桌面应用 (Desktop app)</strong>。</div>
                <div><strong>5. 填入下方：</strong>将生成的 <strong>Client ID</strong> 与 <strong>Client Secret</strong> 复制粘贴到下方，再点击上方的【授权连接 Google 账号】即可一键授权！</div>
              </div>
            ) : null}

            <FormItem label="Client ID" required>
              <Input
                value={draft.clientID}
                onChange={(e) => setDraft({ ...draft, clientID: e.target.value })}
                placeholder="例如 123456789-abcdefg.apps.googleusercontent.com"
              />
            </FormItem>

            <FormItem
              label="Client Secret"
              required={!connection.clientSecretConfigured}
              hint={connection.clientSecretConfigured && !draft.clientSecret ? '（已保存凭据，留空保持不变）' : undefined}
            >
              <Input
                type="password"
                value={draft.clientSecret}
                onChange={(e) => setDraft({ ...draft, clientSecret: e.target.value })}
                placeholder={connection.clientSecretConfigured ? '留空表示保持当前密钥' : 'Google OAuth 客户端密钥'}
              />
            </FormItem>
          </div>

          {/* 存储与网络选项 */}
          <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-4 space-y-3.5">
            <div className="text-sm font-medium text-[var(--color-text-primary)]">存储与网络选项</div>

            <FormItem label="云端备份目录名" hint="保存在 Google 云端硬盘中的文件夹名称">
              <Input
                value={draft.folderName}
                onChange={(e) => setDraft({ ...draft, folderName: e.target.value })}
                placeholder="默认 AntBrowserBackup"
              />
            </FormItem>

            <FormItem label="网络代理（可选）" hint="在中国大陆网络环境下访问 Google 服务建议配置代理">
              <Input
                value={draft.proxyURL}
                onChange={(e) => setDraft({ ...draft, proxyURL: e.target.value })}
                placeholder="例如 http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
              />
            </FormItem>
          </div>
        </div>
      </Modal>

      <ConfirmModal
        open={revokeConfirmOpen}
        title="撤销 Google Drive 访问令牌"
        content="确定要断开与当前 Google 账号的连接并撤销授权令牌吗？这不会删除云端已存在的备份文件。"
        danger
        confirmText="撤销令牌"
        onClose={() => setRevokeConfirmOpen(false)}
        onConfirm={handleRevokeConfirm}
      />
    </>
  )
}
