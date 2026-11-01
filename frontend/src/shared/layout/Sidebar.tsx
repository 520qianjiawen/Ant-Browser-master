import { Link, useLocation } from "react-router-dom";
import {
  Activity,
  Archive,
  Bookmark,
  BookOpen,
  FileText,
  LayoutDashboard,
  ListChecks,
  Monitor,
  Settings,
  Database,
  ChevronLeft,
  ChevronRight,
  Layers,
  PieChart,
  Cpu,
  Globe,
  Bot,
  Puzzle,
  Tag,
  type LucideIcon,
} from "lucide-react";
import clsx from "clsx";
import { useLayoutStore } from "../../store/layoutStore";
import { projectConfig, navigationConfig } from "../../config";

// 导入应用logo
import logoImage from "../../resources/images/logo.png";

const iconMap: Record<string, LucideIcon> = {
  LayoutDashboard,
  Settings,
  Database,
  Layers,
  PieChart,
  Monitor,
  ListChecks,
  Activity,
  Archive,
  FileText,
  Cpu,
  Globe,
  Bot,
  Puzzle,
  Bookmark,
  BookOpen,
  Tag,
};

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPod|iPad/i.test(navigator.platform || navigator.userAgent);

function getIcon(iconName: string): LucideIcon {
  return iconMap[iconName] || LayoutDashboard;
}

export function Sidebar() {
  const location = useLocation();
  const { sidebarCollapsed, toggleSidebar } = useLayoutStore();

  return (
    <aside
      className={clsx(
        "bg-[var(--color-bg-surface)] flex flex-col transition-all duration-300 border-r border-[var(--color-border-default)]",
        sidebarCollapsed ? (isMac ? "w-20" : "w-16") : "w-60",
      )}
    >
      {/* macOS 专属红绿灯保留区 */}
      {isMac && <div className="h-8 w-full wails-drag flex-shrink-0" />}

      {/* 品牌与 Logo 区域 */}
      <div
        className={clsx(
          "flex items-center border-b border-[var(--color-border-muted)] select-none wails-drag",
          sidebarCollapsed
            ? clsx("justify-center", isMac ? "h-12" : "h-14")
            : clsx("px-3.5 gap-2.5", isMac ? "h-12" : "h-14"),
        )}
      >
        {!sidebarCollapsed ? (
          <div className="flex items-center gap-2.5 min-w-0 flex-1">
            <div className="w-[30px] h-[30px] rounded-lg overflow-hidden flex-shrink-0 bg-white shadow-sm border border-[var(--color-border-muted)] flex items-center justify-center p-0.5">
              <img
                src={logoImage}
                alt="应用Logo"
                className="w-full h-full object-contain"
                onError={(e) => {
                  // 图片加载失败时显示首字母
                  e.currentTarget.style.display = "none";
                  e.currentTarget.parentElement?.classList.add("fallback-logo");
                }}
              />
              <span className="text-xs font-bold text-[var(--color-text-inverse)] hidden fallback-content">
                {projectConfig.shortName.charAt(0)}
              </span>
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center justify-between">
                <h2 className="text-sm font-bold text-[var(--color-text-primary)] tracking-tight truncate leading-none">
                  {projectConfig.name}
                </h2>
                <span className="text-[10px] font-semibold px-1.5 py-0.5 rounded bg-[var(--color-accent-muted)] text-[var(--color-accent)] leading-none select-none">
                  v1.8
                </span>
              </div>
            </div>
          </div>
        ) : (
          <div
            className="w-[30px] h-[30px] rounded-lg overflow-hidden flex-shrink-0 bg-white shadow-sm border border-[var(--color-border-muted)] flex items-center justify-center p-0.5"
            title={projectConfig.name}
          >
            <img
              src={logoImage}
              alt="应用Logo"
              className="w-full h-full object-contain"
              onError={(e) => {
                // 图片加载失败时显示首字母
                e.currentTarget.style.display = "none";
                e.currentTarget.parentElement?.classList.add("fallback-logo");
              }}
            />
            <span className="text-xs font-bold text-[var(--color-text-inverse)] hidden fallback-content">
              {projectConfig.shortName.charAt(0)}
            </span>
          </div>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex-1 py-3 px-3 space-y-5 overflow-y-auto">
        {navigationConfig.map((section) => (
          <div key={section.title}>
            {!sidebarCollapsed && (
              <h3 className="px-3 mb-1.5 text-[10px] font-semibold text-[var(--color-text-muted)] uppercase tracking-[0.08em]">
                {section.title}
              </h3>
            )}
            <div className="space-y-0.5">
              {section.items.map((item) => {
                const Icon = getIcon(item.icon);
                const isActive =
                  location.pathname === item.path ||
                  (item.path !== "/" &&
                    location.pathname.startsWith(`${item.path}/`));

                return (
                  <Link
                    key={item.path}
                    to={item.path}
                    title={sidebarCollapsed ? item.name : undefined}
                    className={clsx(
                      "relative flex items-center rounded-lg transition-[background-color,color] duration-150 ease-[var(--ease-out-quart)]",
                      isActive
                        ? "bg-[var(--color-accent)] text-[var(--color-text-inverse)] shadow-[var(--shadow-sm)]"
                        : "text-[var(--color-text-secondary)] hover:bg-[var(--color-accent-muted)] hover:text-[var(--color-text-primary)]",
                      sidebarCollapsed
                        ? "justify-center w-10 h-10 mx-auto"
                        : "px-3 py-2 gap-3",
                    )}
                  >
                    <Icon className={clsx(
                      "w-[18px] h-[18px] flex-shrink-0 transition-transform duration-150",
                      !isActive && "group-hover:scale-105",
                    )} />
                    {!sidebarCollapsed && (
                      <span className={clsx(
                        "text-sm truncate",
                        isActive ? "font-semibold" : "font-medium",
                      )}>
                        {item.name}
                      </span>
                    )}
                  </Link>
                );
              })}
            </div>
          </div>
        ))}
      </nav>

      {/* Toggle Button */}
      <div className="p-3 border-t border-[var(--color-border-muted)]">
        <button
          onClick={toggleSidebar}
          className={clsx(
            "flex items-center rounded-lg text-[var(--color-text-muted)] hover:bg-[var(--color-accent-muted)] hover:text-[var(--color-text-secondary)] transition-all duration-150",
            sidebarCollapsed
              ? "justify-center w-10 h-10 mx-auto"
              : "w-full px-3 py-2 gap-3",
          )}
          title={sidebarCollapsed ? "展开" : "收起"}
        >
          {sidebarCollapsed ? (
            <ChevronRight className="w-[18px] h-[18px]" />
          ) : (
            <>
              <ChevronLeft className="w-[18px] h-[18px]" />
              <span className="text-sm">收起侧边栏</span>
            </>
          )}
        </button>
      </div>
    </aside>
  );
}
