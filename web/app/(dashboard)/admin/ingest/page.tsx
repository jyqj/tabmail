"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { formatDistanceToNow } from "date-fns";
import { Boxes, RefreshCw, Search } from "lucide-react";
import { toast } from "sonner";

import { listIngestJobs } from "@/lib/api";
import type { IngestJob } from "@/lib/types";
import { PageHeader } from "@/components/layout/page-header";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const PAGE_SIZE = 30;

const stateStyles: Record<string, string> = {
  pending: "border-slate-500/20 bg-slate-500/10 text-slate-700",
  processing: "border-sky-500/20 bg-sky-500/10 text-sky-700",
  retry: "border-amber-500/20 bg-amber-500/10 text-amber-700",
  done: "border-emerald-500/20 bg-emerald-500/10 text-emerald-700",
  dead: "border-rose-500/20 bg-rose-500/10 text-rose-700",
};

export default function AdminIngestPage() {
  const [jobs, setJobs] = useState<IngestJob[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [state, setState] = useState("all");
  const [source, setSource] = useState("all");
  const [recipient, setRecipient] = useState("");

  const fetchJobs = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listIngestJobs({
        page,
        per_page: PAGE_SIZE,
        state: state === "all" ? undefined : state,
        source: source === "all" ? undefined : source,
        recipient: recipient || undefined,
      });
      setJobs(res.data);
      setTotal(res.meta.total);
    } catch {
      toast.error("加载 ingest jobs 失败");
    } finally {
      setLoading(false);
    }
  }, [page, recipient, source, state]);

  useEffect(() => {
    fetchJobs();
  }, [fetchJobs]);

  const totals = useMemo(() => {
    return jobs.reduce<Record<string, number>>((acc, item) => {
      acc[item.state] = (acc[item.state] || 0) + 1;
      return acc;
    }, {});
  }, [jobs]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="flex flex-col">
      <PageHeader
        title="Ingest Jobs"
        description="查看 durable ingest 队列、重试状态与当前 backlog。"
        actions={
          <Button variant="outline" size="sm" className="gap-1.5" onClick={fetchJobs}>
            <RefreshCw className="h-3.5 w-3.5" />
            刷新
          </Button>
        }
      />

      <div className="space-y-4 p-4">
        <div className="grid gap-4 md:grid-cols-4">
          <StatCard title="当前页" value={jobs.length} />
          <StatCard title="总数" value={total} />
          <StatCard title="处理中" value={totals.processing || 0} />
          <StatCard title="重试/死信" value={(totals.retry || 0) + (totals.dead || 0)} />
        </div>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Search className="h-4 w-4 text-primary" />
              筛选
            </CardTitle>
            <CardDescription>按状态、来源和收件人筛选 ingest jobs。</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-3">
            <Select value={state} onValueChange={(value) => { setPage(1); setState(value || "all"); }}>
              <SelectTrigger>
                <SelectValue placeholder="状态" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="pending">pending</SelectItem>
                <SelectItem value="processing">processing</SelectItem>
                <SelectItem value="retry">retry</SelectItem>
                <SelectItem value="done">done</SelectItem>
                <SelectItem value="dead">dead</SelectItem>
              </SelectContent>
            </Select>
            <Select value={source} onValueChange={(value) => { setPage(1); setSource(value || "all"); }}>
              <SelectTrigger>
                <SelectValue placeholder="来源" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部来源</SelectItem>
                <SelectItem value="smtp">smtp</SelectItem>
              </SelectContent>
            </Select>
            <Input
              placeholder="recipient@example.com"
              value={recipient}
              onChange={(e) => {
                setPage(1);
                setRecipient(e.target.value);
              }}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Boxes className="h-4 w-4 text-primary" />
              队列明细
            </CardTitle>
            <CardDescription>最近创建的 ingest job 会先排队，再由 worker 异步处理。</CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 8 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : jobs.length === 0 ? (
              <div className="py-12 text-center text-sm text-muted-foreground">暂无 ingest jobs</div>
            ) : (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>状态</TableHead>
                      <TableHead>来源</TableHead>
                      <TableHead>发件人</TableHead>
                      <TableHead>收件人</TableHead>
                      <TableHead>尝试</TableHead>
                      <TableHead>下次重试</TableHead>
                      <TableHead>创建时间</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {jobs.map((job) => (
                      <TableRow key={job.id}>
                        <TableCell>
                          <Badge variant="outline" className={stateStyles[job.state] || ""}>
                            {job.state}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-sm">{job.source}</TableCell>
                        <TableCell className="max-w-[220px] truncate text-sm">{job.mail_from || "—"}</TableCell>
                        <TableCell className="max-w-[280px] truncate text-sm">
                          {job.recipients.join(", ")}
                        </TableCell>
                        <TableCell className="text-sm">{job.attempts}</TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {job.state === "retry" || job.state === "processing"
                            ? formatDistanceToNow(new Date(job.next_attempt_at), { addSuffix: true })
                            : "—"}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(job.created_at), { addSuffix: true })}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <div className="mt-4 space-y-2">
                  {jobs.filter((job) => job.last_error).slice(0, 5).map((job) => (
                    <div key={`${job.id}-err`} className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-3 text-xs">
                      <div className="font-medium text-foreground">{job.id}</div>
                      <div className="mt-1 break-all text-muted-foreground">{job.last_error}</div>
                    </div>
                  ))}
                </div>
                <Pager page={page} totalPages={totalPages} onPageChange={setPage} />
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function StatCard({ title, value }: { title: string; value: number }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-3xl font-bold tracking-tight">{value.toLocaleString()}</div>
      </CardContent>
    </Card>
  );
}

function Pager({
  page,
  totalPages,
  onPageChange,
}: {
  page: number;
  totalPages: number;
  onPageChange: (page: number) => void;
}) {
  return (
    <div className="mt-4 flex items-center justify-between">
      <div className="text-xs text-muted-foreground">
        第 {page} / {totalPages} 页
      </div>
      <div className="flex gap-2">
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
          上一页
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={page >= totalPages}
          onClick={() => onPageChange(page + 1)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}
