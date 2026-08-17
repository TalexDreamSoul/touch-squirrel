import Link from "next/link";

// An explicit App Router 404. Without it, `output: "export"` falls back to the
// pages-router error document, which fails to prerender ("<Html> should not be
// imported outside of pages/_document") and aborts the whole export.
export default function NotFound() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-2xl font-medium">页面不存在</h1>
      <p className="text-sm opacity-70">地址可能已变更，或该页面尚未部署。</p>
      <Link href="/" className="text-sm underline">
        返回概览
      </Link>
    </main>
  );
}
