"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";

export default function ArtifactsRedirectPage() {
  const router = useRouter();

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    params.set("tab", "artifacts");
    router.replace(`/pool/?${params.toString()}`);
  }, [router]);

  return (
    <AdminShell>
      <Text variant="secondary">正在打开凭证池的原始产物…</Text>
    </AdminShell>
  );
}
