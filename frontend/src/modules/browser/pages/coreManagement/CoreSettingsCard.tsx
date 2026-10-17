import {
  Clock,
  Edit2,
  Folder,
  Globe,
  RotateCcw,
  Settings,
  Sliders,
  Terminal,
  Trash2,
  Zap,
} from 'lucide-react'
import { Badge, Button, Card } from '../../../../shared/components'
import type { BrowserSettings } from '../../types'

interface CoreSettingsCardProps {
  settings: BrowserSettings
  onEdit: () => void
}

export function CoreSettingsCard({ settings, onEdit }: CoreSettingsCardProps) {
  return (
    <Card>
      {/* 头部标题与编辑操作 */}
      <div className="flex items-center justify-between pb-3.5 border-b border-[var(--color-border-subtle)] mb-4">
        <div className="flex items-center gap-2.5">
          <div className="w-8 h-8 rounded-xl bg-[var(--color-accent-muted)] flex items-center justify-center text-[var(--color-accent)] shadow-xs">
            <Settings className="w-4 h-4" />
          </div>
          <div>
            <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">全局默认设置</h3>
            <p className="text-xs text-[var(--color-text-muted)]">管理浏览器实例的默认启动参数、数据存储路径与运行策略</p>
          </div>
        </div>
        <Button size="sm" variant="secondary" onClick={onEdit} className="gap-1.5 shadow-xs">
          <Edit2 className="w-3.5 h-3.5" />
          <span>编辑设置</span>
        </Button>
      </div>

      <div className="space-y-3.5">
        {/* 第一行：运行特性开关（三列整齐排列，高度完全一致） */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          {/* 轻启动模式 */}
          <div className="flex items-center justify-between p-3 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] hover:border-[var(--color-border-strong)] transition-all shadow-xs">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="w-7 h-7 rounded-lg bg-amber-500/10 text-amber-600 dark:text-amber-400 flex items-center justify-center shrink-0">
                <Zap className="w-3.5 h-3.5" />
              </div>
              <div className="truncate">
                <div className="text-xs font-semibold text-[var(--color-text-primary)]">轻启动模式</div>
                <div className="text-[11px] text-[var(--color-text-muted)] truncate">极速启动首个页面</div>
              </div>
            </div>
            <Badge variant={settings.lightStartEnabled ? 'success' : 'default'} size="sm" dot>
              {settings.lightStartEnabled ? '已开启' : '已关闭'}
            </Badge>
          </div>

          {/* 恢复历史标签 */}
          <div className="flex items-center justify-between p-3 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] hover:border-[var(--color-border-strong)] transition-all shadow-xs">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="w-7 h-7 rounded-lg bg-blue-500/10 text-blue-600 dark:text-blue-400 flex items-center justify-center shrink-0">
                <RotateCcw className="w-3.5 h-3.5" />
              </div>
              <div className="truncate">
                <div className="text-xs font-semibold text-[var(--color-text-primary)]">恢复历史标签</div>
                <div className="text-[11px] text-[var(--color-text-muted)] truncate">重新打开上次会话</div>
              </div>
            </div>
            <Badge variant={settings.restoreLastSession ? 'success' : 'default'} size="sm" dot>
              {settings.restoreLastSession ? '已开启' : '已关闭'}
            </Badge>
          </div>

          {/* 退出清理缓存 */}
          <div className="flex items-center justify-between p-3 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] hover:border-[var(--color-border-strong)] transition-all shadow-xs">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="w-7 h-7 rounded-lg bg-red-500/10 text-red-600 dark:text-red-400 flex items-center justify-center shrink-0">
                <Trash2 className="w-3.5 h-3.5" />
              </div>
              <div className="truncate">
                <div className="text-xs font-semibold text-[var(--color-text-primary)]">退出清理缓存</div>
                <div className="text-[11px] text-[var(--color-text-muted)] truncate">关闭时清理临时数据</div>
              </div>
            </div>
            <Badge variant={settings.clearCacheOnClose ? 'success' : 'default'} size="sm" dot>
              {settings.clearCacheOnClose ? '已开启' : '已关闭'}
            </Badge>
          </div>
        </div>

        {/* 第二行：核心参数与阈值（4列等高规整卡片，单位水平对齐不折行） */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          {/* 用户数据根目录 */}
          <div className="p-3 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-muted)]/40 space-y-1">
            <div className="flex items-center gap-1.5 text-xs text-[var(--color-text-muted)] font-medium">
              <Folder className="w-3.5 h-3.5" />
              <span>用户数据根目录</span>
            </div>
            <div
              className="font-mono text-xs text-[var(--color-text-primary)] font-semibold truncate py-0.5"
              title={settings.userDataRoot || 'data'}
            >
              {settings.userDataRoot || 'data'}
            </div>
          </div>

          {/* 启动就绪超时 */}
          <div className="p-3 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-muted)]/40 space-y-1">
            <div className="flex items-center gap-1.5 text-xs text-[var(--color-text-muted)] font-medium">
              <Clock className="w-3.5 h-3.5" />
              <span>启动就绪超时</span>
            </div>
            <div className="text-xs text-[var(--color-text-primary)] py-0.5 flex items-baseline gap-1">
              <span className="font-mono font-semibold text-sm">{settings.startReadyTimeoutMs}</span>
              <span className="text-[var(--color-text-muted)] text-xs">ms</span>
            </div>
          </div>

          {/* 启动稳定窗口 */}
          <div className="p-3 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-muted)]/40 space-y-1">
            <div className="flex items-center gap-1.5 text-xs text-[var(--color-text-muted)] font-medium">
              <Clock className="w-3.5 h-3.5" />
              <span>启动稳定窗口</span>
            </div>
            <div className="text-xs text-[var(--color-text-primary)] py-0.5 flex items-baseline gap-1">
              <span className="font-mono font-semibold text-sm">{settings.startStableWindowMs}</span>
              <span className="text-[var(--color-text-muted)] text-xs">ms</span>
            </div>
          </div>

          {/* 默认启动页面 */}
          <div className="p-3 rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-muted)]/40 space-y-1">
            <div className="flex items-center gap-1.5 text-xs text-[var(--color-text-muted)] font-medium">
              <Globe className="w-3.5 h-3.5" />
              <span>默认启动页面</span>
            </div>
            <div
              className="font-mono text-xs text-[var(--color-accent)] font-semibold truncate py-0.5"
              title={settings.defaultStartUrls?.join(', ') || 'https://www.google.com/'}
            >
              {settings.defaultStartUrls?.[0] || 'https://www.google.com/'}
              {settings.defaultStartUrls && settings.defaultStartUrls.length > 1 && (
                <span className="text-[var(--color-text-muted)] font-normal text-[10px] ml-1">
                  (+{settings.defaultStartUrls.length - 1})
                </span>
              )}
            </div>
          </div>
        </div>

        {/* 第三行：默认指纹参数与启动参数（左右两栏平分，精致代码列表） */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3 pt-0.5">
          {/* 默认指纹参数 */}
          <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-muted)]/30 p-3 space-y-2">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-1.5 text-xs font-semibold text-[var(--color-text-secondary)]">
                <Sliders className="w-3.5 h-3.5 text-[var(--color-accent)]" />
                <span>默认指纹参数</span>
              </div>
              <span className="text-[11px] text-[var(--color-text-muted)] font-mono">
                {settings.defaultFingerprintArgs?.length || 0} 项
              </span>
            </div>
            {settings.defaultFingerprintArgs && settings.defaultFingerprintArgs.length > 0 ? (
              <div className="max-h-28 overflow-y-auto space-y-1 pr-1">
                {settings.defaultFingerprintArgs.map((arg, idx) => (
                  <div
                    key={idx}
                    className="font-mono text-[11px] px-2.5 py-1 rounded-lg bg-[var(--color-bg-surface)] border border-[var(--color-border-subtle)] text-[var(--color-text-primary)] truncate hover:border-[var(--color-border-default)] transition-colors"
                    title={arg}
                  >
                    {arg}
                  </div>
                ))}
              </div>
            ) : (
              <div className="text-xs text-[var(--color-text-muted)] py-2 text-center">
                未配置默认指纹参数
              </div>
            )}
          </div>

          {/* 默认启动参数 */}
          <div className="rounded-xl border border-[var(--color-border-default)] bg-[var(--color-bg-muted)]/30 p-3 space-y-2">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-1.5 text-xs font-semibold text-[var(--color-text-secondary)]">
                <Terminal className="w-3.5 h-3.5 text-[var(--color-accent)]" />
                <span>默认启动参数</span>
              </div>
              <span className="text-[11px] text-[var(--color-text-muted)] font-mono">
                {settings.defaultLaunchArgs?.length || 0} 项
              </span>
            </div>
            {settings.defaultLaunchArgs && settings.defaultLaunchArgs.length > 0 ? (
              <div className="max-h-28 overflow-y-auto space-y-1 pr-1">
                {settings.defaultLaunchArgs.map((arg, idx) => (
                  <div
                    key={idx}
                    className="font-mono text-[11px] px-2.5 py-1 rounded-lg bg-[var(--color-bg-surface)] border border-[var(--color-border-subtle)] text-[var(--color-text-primary)] truncate hover:border-[var(--color-border-default)] transition-colors"
                    title={arg}
                  >
                    {arg}
                  </div>
                ))}
              </div>
            ) : (
              <div className="text-xs text-[var(--color-text-muted)] py-2 text-center">
                未配置默认启动参数
              </div>
            )}
          </div>
        </div>
      </div>
    </Card>
  )
}
