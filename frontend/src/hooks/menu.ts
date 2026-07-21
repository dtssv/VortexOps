import type { Menu } from '@/types';

/** Flatten a possibly-nested menu structure into a flat list. */
function flattenMenus(menus: Menu[]): Menu[] {
  const out: Menu[] = [];
  const walk = (nodes: Menu[]) => {
    for (const n of nodes) {
      out.push(n);
      if (n.children?.length) walk(n.children);
    }
  };
  walk(menus);
  return out;
}

/** Build a tree from a flat OR nested menu list using parent_id. */
// 后端 /me/menus 可能返回已建好的树（节点带 children），也可能返回扁平列表。
// 先递归扁平化，再按 parent_id 重建，确保两种输入都能正确建树，且 children 不会被意外清空。
export function buildMenuTree(menus: Menu[]): Menu[] {
  const flat = flattenMenus(menus);
  const map = new Map<number, Menu>();
  const roots: Menu[] = [];
  flat.forEach((m) => map.set(m.id, { ...m, children: [] }));
  map.forEach((m) => {
    if (m.parent_id && map.has(m.parent_id)) {
      map.get(m.parent_id)!.children!.push(m);
    } else {
      roots.push(m);
    }
  });
  const sortRec = (nodes: Menu[]) => {
    nodes.sort((a, b) => a.sort_order - b.sort_order);
    nodes.forEach((n) => n.children && sortRec(n.children));
  };
  sortRec(roots);
  return roots;
}

/** Flatten menu tree to a list of menu items that have a path (leaves with routes). */
export function flattenMenuPaths(menus: Menu[]): Menu[] {
  const out: Menu[] = [];
  const walk = (nodes: Menu[]) => {
    for (const n of nodes) {
      if (n.path && n.menu_type !== 'button') out.push(n);
      if (n.children?.length) walk(n.children);
    }
  };
  walk(menus);
  return out;
}
