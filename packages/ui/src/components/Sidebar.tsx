import { useMemo, useCallback, type ReactNode, type ComponentType } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import './Sidebar.css';

export interface SidebarItem {
  id: string;
  icon: ComponentType<{ size?: number; className?: string }>;
  label: string;
  badge?: number | string;
  section?: string;
  disabled?: boolean;
}

export interface SidebarSection {
  title?: string;
  items: SidebarItem[];
}

export interface SidebarProps {
  items?: SidebarItem[];
  sections?: SidebarSection[];
  activeItem?: string;
  onItemClick?: (itemId: string) => void;
  collapsed?: boolean;
  onToggleCollapse?: () => void;
  headerContent?: ReactNode;
  footerContent?: ReactNode;
  width?: number;
  collapsedWidth?: number;
  className?: string;
}

/**
 * Individual sidebar item component.
 */
function SidebarItemComponent({
  item,
  isActive,
  isCollapsed,
  onClick,
}: {
  item: SidebarItem;
  isActive: boolean;
  isCollapsed: boolean;
  onClick: () => void;
}): JSX.Element {
  const Icon = item.icon;
  const hasBadge = item.badge !== undefined;

  return (
    <button
      type="button"
      className={`sidebar-item ${isActive ? 'sidebar-item-active' : ''} ${item.disabled ? 'sidebar-item-disabled' : ''}`}
      onClick={onClick}
      disabled={item.disabled}
      title={isCollapsed ? item.label : undefined}
      aria-label={item.label}
      aria-current={isActive ? 'page' : undefined}
    >
      <span className="sidebar-item-icon" aria-hidden="true">
        <Icon size={18} />
      </span>
      {!isCollapsed && (
        <>
          <span className="sidebar-item-label">{item.label}</span>
          {hasBadge && (
            <span className="sidebar-item-badge">
              {typeof item.badge === 'number' && item.badge > 99 ? '99+' : item.badge}
            </span>
          )}
        </>
      )}
    </button>
  );
}

/**
 * An icon-based navigation sidebar.
 *
 * Features vertical icon strip with labels, active state, expand/collapse,
 * sections with dividers, badges, header/footer areas, and tooltips when collapsed.
 */
function Sidebar({
  items: flatItems,
  sections,
  activeItem,
  onItemClick,
  collapsed = false,
  onToggleCollapse,
  headerContent,
  footerContent,
  width = 240,
  collapsedWidth = 48,
  className,
}: SidebarProps): JSX.Element {
  // Use sections if provided, otherwise convert flat items to single section
  const sidebarSections: SidebarSection[] = useMemo(() => {
    if (sections && sections.length > 0) {
      return sections;
    }
    if (flatItems && flatItems.length > 0) {
      return [{ items: flatItems }];
    }
    return [];
  }, [sections, flatItems]);

  const currentWidth = collapsed ? collapsedWidth : width;

  const handleItemClick = useCallback(
    (itemId: string) => {
      if (onItemClick) {
        onItemClick(itemId);
      }
    },
    [onItemClick],
  );

  const handleToggleCollapse = useCallback(() => {
    if (onToggleCollapse) {
      onToggleCollapse();
    }
  }, [onToggleCollapse]);

  return (
    <aside
      className={`sidebar ${collapsed ? 'sidebar-collapsed' : ''} ${className || ''}`}
      style={{ width: `${currentWidth}px` }}
      role="navigation"
      aria-label="Sidebar navigation"
    >
      {/* Header */}
      {headerContent && <div className="sidebar-header">{headerContent}</div>}

      {/* Navigation Items */}
      <nav className="sidebar-nav">
        {sidebarSections.map((section, sectionIndex) => (
          <div key={sectionIndex} className="sidebar-section">
            {/* Section Title */}
            {section.title && !collapsed && (
              <div className="sidebar-section-title">{section.title}</div>
            )}

            {/* Section Items */}
            <div className="sidebar-section-items">
              {section.items.map((item) => (
                <SidebarItemComponent
                  key={item.id}
                  item={item}
                  isActive={activeItem === item.id}
                  isCollapsed={collapsed}
                  onClick={() => handleItemClick(item.id)}
                />
              ))}
            </div>
          </div>
        ))}
      </nav>

      {/* Footer with Collapse Toggle */}
      <div className="sidebar-footer">
        {footerContent}
        {onToggleCollapse && (
          <button
            type="button"
            className="sidebar-collapse"
            onClick={handleToggleCollapse}
            aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            aria-expanded={!collapsed}
          >
            {collapsed ? <ChevronRight size={16} /> : <ChevronLeft size={16} />}
          </button>
        )}
      </div>
    </aside>
  );
}

export default Sidebar;
