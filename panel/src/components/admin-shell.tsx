"use client";

import Image from "next/image";
import { usePathname, useRouter } from "next/navigation";
import {
  ArchiveIcon,
  BellIcon,
  BookOpenIcon,
  BinocularsIcon,
  BroadcastIcon,
  CloudArrowUpIcon,
  DownloadSimpleIcon,
  GearIcon,
  HouseIcon,
  NetworkIcon,
  PlayCircleIcon,
  PlugsIcon,
  PulseIcon,
  SignOutIcon,
  StackIcon,
} from "@phosphor-icons/react";
import {
  Button,
  Loader,
  Sidebar,
  Surface,
  Text,
} from "@cloudflare/kumo";
import type { ReactNode } from "react";
import { useAuth } from "@/lib/auth";
import { Onboarding } from "@/components/onboarding";

type NavItem = {
  href: string;
  label: string;
  icon: typeof HouseIcon;
};

type NavGroup = {
  label: string;
  items: NavItem[];
};

const navGroups: NavGroup[] = [
  {
    label: "工作台",
    items: [
      { href: "/", label: "概览", icon: HouseIcon },
      { href: "/register", label: "注册", icon: PlayCircleIcon },
    ],
  },
  {
    label: "Host",
    items: [
      { href: "/plugins", label: "插件", icon: PlugsIcon },
      { href: "/artifacts", label: "囤货", icon: ArchiveIcon },
    ],
  },
  {
    label: "凭证",
    items: [
      { href: "/upload", label: "上传", icon: CloudArrowUpIcon },
      { href: "/export", label: "导出", icon: DownloadSimpleIcon },
      { href: "/pool", label: "号池", icon: StackIcon },
    ],
  },
  {
    label: "风控",
    items: [{ href: "/degrade", label: "降智分析", icon: PulseIcon }],
  },
  {
    label: "联邦",
    items: [{ href: "/cluster", label: "主从", icon: NetworkIcon }],
  },
  {
    label: "状态页",
    items: [
      { href: "/status", label: "公开看板", icon: BroadcastIcon },
      { href: "/status-admin", label: "看板设置", icon: GearIcon },
    ],
  },
  {
    label: "安全",
    items: [{ href: "/hunter", label: "泄露巡检", icon: BinocularsIcon }],
  },
  {
    label: "文档",
    items: [{ href: "/docs", label: "技术说明", icon: BookOpenIcon }],
  },
  {
    label: "系统",
    items: [
      { href: "/notifications", label: "通知", icon: BellIcon },
      { href: "/settings", label: "设置", icon: GearIcon },
    ],
  },
];

export function AdminShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { loading, authed, logout } = useAuth();

  if (loading) {
    return (
      <Surface className="flex min-h-screen items-center justify-center gap-2">
        <Loader />
        <Text variant="secondary">加载中…</Text>
      </Surface>
    );
  }

  if (!authed) {
    if (typeof window !== "undefined" && pathname !== "/login/") {
      router.replace("/login/");
    }
    return (
      <Surface className="flex min-h-screen items-center justify-center">
        <Text variant="secondary">跳转登录…</Text>
      </Surface>
    );
  }

  return (
    <Sidebar.Provider
      defaultOpen
      defaultWidth={248}
      minWidth={220}
      maxWidth={320}
      className="min-h-screen"
    >
      <Sidebar className="flex h-screen flex-col">
        <Sidebar.Header className="!flex !w-full !items-center !justify-between !gap-2">
          <div className="flex min-w-0 flex-1 items-center gap-3 overflow-hidden">
            <Image
              src="/brand/squirrel-icon.png"
              alt="松果仓"
              width={40}
              height={40}
              priority
              className="size-10 shrink-0 rounded-lg"
            />
            <div className="min-w-0">
              <Text variant="heading3" as="span">
                松果仓
              </Text>
              <Text variant="secondary" size="xs">
                插件 · 注册 · 号池
              </Text>
            </div>
          </div>
          <Sidebar.Trigger className="shrink-0" />
        </Sidebar.Header>

        <Sidebar.Content className="flex-1 overflow-y-auto">
          {navGroups.map((group) => (
            <Sidebar.Group key={group.label}>
              <Sidebar.GroupLabel>{group.label}</Sidebar.GroupLabel>
              <Sidebar.Menu>
                {group.items.map((item) => {
                  const active =
                    item.href === "/"
                      ? pathname === "/" || pathname === ""
                      : pathname === item.href ||
                        pathname.startsWith(`${item.href}/`);
                  return (
                    <Sidebar.MenuItem key={item.href}>
                      <Sidebar.MenuButton
                        icon={item.icon}
                        active={active}
                        onClick={() => router.push(item.href)}
                        title={item.label}
                        className="!whitespace-nowrap"
                      >
                        {item.label}
                      </Sidebar.MenuButton>
                    </Sidebar.MenuItem>
                  );
                })}
              </Sidebar.Menu>
            </Sidebar.Group>
          ))}
        </Sidebar.Content>

        <Sidebar.Footer className="!h-auto !min-h-0 !w-full !flex-col !items-stretch !gap-2 !overflow-visible !px-2 !py-2">
          <Button
            variant="ghost"
            size="sm"
            className="!w-full !justify-start"
            onClick={() => {
              logout();
              router.replace("/login/");
            }}
          >
            <SignOutIcon size={16} /> 退出登录
          </Button>
        </Sidebar.Footer>
      </Sidebar>

      <main className="min-h-screen flex-1 overflow-auto p-6">
        <div className="mb-3 md:hidden">
          <Sidebar.Trigger title="打开导航" />
        </div>
        {children}
      </main>

      <Onboarding />
    </Sidebar.Provider>
  );
}
