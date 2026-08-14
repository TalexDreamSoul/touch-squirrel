"use client";

import { useRef, useState, type ReactNode } from "react";
import { Badge, LayerCard, Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { chapters } from "./chapters.generated";

type Chapter = (typeof chapters)[number];
type ChapterID = Chapter["id"];

export default function DocsPage() {
  const [activeChapterID, setActiveChapterID] = useState<ChapterID>("overview");
  const [showStory, setShowStory] = useState(false);
  const articleRef = useRef<HTMLElement>(null);
  const activeIndex = chapters.findIndex((chapter) => chapter.id === activeChapterID);
  const activeChapter = chapters[activeIndex];
  const previousChapter = chapters[activeIndex - 1];
  const nextChapter = chapters[activeIndex + 1];

  const selectChapter = (id: ChapterID) => {
    setActiveChapterID(id);
    setShowStory(false);
    requestAnimationFrame(() => articleRef.current?.scrollIntoView({ behavior: "smooth", block: "start" }));
  };

  return (
    <AdminShell>
      <PageHeader
        title="用户手册与技术原理"
        description="每章由独立 Markdown 文件提供内容，并在构建时生成可检索索引。"
      />

      <div className="flex max-w-[1200px] flex-col items-start gap-6 lg:flex-row">
        <aside className="w-full shrink-0 lg:sticky lg:top-5 lg:w-52 lg:max-h-[calc(100vh-2.5rem)] lg:overflow-y-auto">
          <LayerCard>
            <LayerCard.Secondary>章节</LayerCard.Secondary>
            <LayerCard.Primary>
              <nav aria-label="文档章节" className="flex gap-1 overflow-x-auto lg:flex-col lg:overflow-visible">
                {chapters.map((chapter) => {
                  const selected = activeChapter.id === chapter.id;
                  return (
                    <button
                      aria-current={selected ? "page" : undefined}
                      className={`shrink-0 rounded-md px-3 py-2 text-left text-sm transition-colors ${selected ? "bg-black/10 font-medium dark:bg-white/10" : "hover:bg-black/5 dark:hover:bg-white/5"}`}
                      key={chapter.id}
                      onClick={() => selectChapter(chapter.id)}
                      type="button"
                    >
                      {chapter.label}
                    </button>
                  );
                })}
              </nav>
            </LayerCard.Primary>
          </LayerCard>
        </aside>

        <article ref={articleRef} className="min-w-0 flex-1 scroll-mt-5">
          <LayerCard>
            <LayerCard.Secondary>
              <div className="flex items-center justify-between gap-3">
                <span>第 {activeChapter.order} 章 · {activeChapter.label}</span>
                <Badge variant="secondary">预计阅读 {activeChapter.estimate}</Badge>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary>
              <MarkdownRenderer content={activeChapter.content} />

              {showStory && (
                <div className="mt-6 border-t border-black/10 pt-6 dark:border-white/10">
                  <MarkdownRenderer content={activeChapter.story} />
                </div>
              )}

              <footer className="mt-8 flex flex-wrap items-center justify-between gap-3 border-t border-black/10 pt-5 dark:border-white/10">
                <button
                  className="rounded-md px-3 py-2 text-sm transition-colors hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:bg-white/5"
                  disabled={!previousChapter}
                  onClick={() => previousChapter && selectChapter(previousChapter.id)}
                  type="button"
                >
                  ← 上一章
                </button>
                <button
                  className="rounded-md bg-black/10 px-3 py-2 text-sm font-medium transition-colors hover:bg-black/15 dark:bg-white/10 dark:hover:bg-white/15"
                  onClick={() => setShowStory((value) => !value)}
                  type="button"
                >
                  {showStory ? "收起故事" : "阅读故事"}
                </button>
                <button
                  className="rounded-md px-3 py-2 text-sm transition-colors hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-40 dark:hover:bg-white/5"
                  disabled={!nextChapter}
                  onClick={() => nextChapter && selectChapter(nextChapter.id)}
                  type="button"
                >
                  下一章 →
                </button>
              </footer>
            </LayerCard.Primary>
          </LayerCard>
        </article>
      </div>
    </AdminShell>
  );
}

function MarkdownRenderer({ content }: { content: string }) {
  return (
    <div className="flex flex-col gap-4">
      {content.trim().split("\n\n").map((block, index) => {
        if (block.startsWith("## ")) {
          return <Text as="h2" key={index} variant="heading3">{block.slice(3)}</Text>;
        }
        if (block.startsWith("- ")) {
          return (
            <ul className="flex list-disc flex-col gap-1 pl-5 text-sm text-black/70 dark:text-white/70" key={index}>
              {block.split("\n").map((item) => <li key={item}>{renderInline(item.slice(2))}</li>)}
            </ul>
          );
        }
        return <Text key={index} size="sm" variant="secondary">{renderInline(block)}</Text>;
      })}
    </div>
  );
}

function renderInline(value: string): ReactNode[] {
  return value.split(/(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^\)]+\))/g).filter(Boolean).map((part, index) => {
    if (part.startsWith("**") && part.endsWith("**")) return <strong key={index}>{part.slice(2, -2)}</strong>;
    if (part.startsWith("`") && part.endsWith("`")) return <code className="rounded bg-black/10 px-1 py-0.5 text-[0.85em] dark:bg-white/10" key={index}>{part.slice(1, -1)}</code>;
    const link = part.match(/^\[([^\]]+)\]\(([^\)]+)\)$/);
    if (link) return <a className="underline underline-offset-2" href={link[2]} key={index} rel="noreferrer" target="_blank">{link[1]}</a>;
    return <span key={index}>{part}</span>;
  });
}
