"use client";

import { useState, useEffect, useCallback } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { listMailboxes, createMailbox, deleteMailbox } from "@/lib/api";
import type { Mailbox, AccessMode } from "@/lib/types";
import {
  Plus,
  MoreHorizontal,
  Trash2,
  ExternalLink,
  Inbox,
  Copy,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import Link from "next/link";

const accessColors: Record<AccessMode, string> = {
  public: "bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400",
  token: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400",
  api_key: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400",
};

export default function MailboxesPage() {
  const [mailboxes, setMailboxes] = useState<Mailbox[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  const [newAddress, setNewAddress] = useState("");
  const [newAccessMode, setNewAccessMode] = useState<AccessMode>("public");
  const [newPassword, setNewPassword] = useState("");

  const fetchMailboxes = useCallback(async () => {
    try {
      const res = await listMailboxes(page);
      setMailboxes(res.data);
      setTotal(res.meta.total);
    } catch {
      toast.error("Failed to load mailboxes");
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => {
    fetchMailboxes();
  }, [fetchMailboxes]);

  const handleCreate = async () => {
    if (!newAddress.trim()) return;
    setCreating(true);
    try {
      await createMailbox({
        address: newAddress.trim(),
        access_mode: newAccessMode,
        password: newAccessMode === "token" ? newPassword : undefined,
      });
      setNewAddress("");
      setNewPassword("");
      setDialogOpen(false);
      toast.success("Mailbox created");
      fetchMailboxes();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to create mailbox");
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteMailbox(id);
      toast.success("Mailbox deleted");
      fetchMailboxes();
    } catch {
      toast.error("Failed to delete");
    }
  };

  const totalPages = Math.ceil(total / 30);

  return (
    <div className="flex flex-col">
      <PageHeader
        title="Mailboxes"
        description={`${total} mailbox${total !== 1 ? "es" : ""}`}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              Create Mailbox
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>Create Mailbox</DialogTitle>
                <DialogDescription>
                  Create a new mailbox on a verified domain.
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label>Full Address</Label>
                  <Input
                    placeholder="user@mail.example.com"
                    value={newAddress}
                    onChange={(e) => setNewAddress(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && handleCreate()}
                  />
                </div>
                <div className="space-y-2">
                  <Label>Access Mode</Label>
                  <Select
                    value={newAccessMode}
                    onValueChange={(v) => setNewAccessMode(v as AccessMode)}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="public">Public</SelectItem>
                      <SelectItem value="token">Token (password required)</SelectItem>
                      <SelectItem value="api_key">API Key only</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                {newAccessMode === "token" && (
                  <div className="space-y-2">
                    <Label>Password</Label>
                    <Input
                      type="password"
                      placeholder="Mailbox password"
                      value={newPassword}
                      onChange={(e) => setNewPassword(e.target.value)}
                    />
                  </div>
                )}
              </div>
              <DialogFooter>
                <Button onClick={handleCreate} disabled={creating || !newAddress.trim()}>
                  {creating ? "Creating..." : "Create"}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">All Mailboxes</CardTitle>
            <CardDescription>
              Manage mailboxes across your domains.
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : mailboxes.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Inbox className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">No mailboxes yet</p>
                <p className="text-xs mt-1">
                  Create a mailbox or they will be auto-created by routes
                </p>
              </div>
            ) : (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Address</TableHead>
                      <TableHead>Access Mode</TableHead>
                      <TableHead>Domain</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead className="w-10" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {mailboxes.map((mb) => (
                      <TableRow key={mb.id}>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <code className="text-sm font-medium">
                              {mb.full_address}
                            </code>
                            <button
                              onClick={() => {
                                navigator.clipboard.writeText(mb.full_address);
                                toast.success("Copied");
                              }}
                              className="text-muted-foreground hover:text-foreground"
                            >
                              <Copy className="h-3 w-3" />
                            </button>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant="secondary"
                            className={accessColors[mb.access_mode]}
                          >
                            {mb.access_mode}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {mb.resolved_domain}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(mb.created_at), {
                            addSuffix: true,
                          })}
                        </TableCell>
                        <TableCell>
                          <DropdownMenu>
                            <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-8 w-8" />}>
                              <MoreHorizontal className="h-4 w-4" />
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem render={<Link href={`/inbox/${encodeURIComponent(mb.full_address)}`} />}>
                                <ExternalLink className="h-4 w-4 mr-2" />
                                Open Inbox
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                onClick={() => handleDelete(mb.id)}
                                className="text-destructive focus:text-destructive"
                              >
                                <Trash2 className="h-4 w-4 mr-2" />
                                Delete
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>

                {totalPages > 1 && (
                  <div className="flex justify-center gap-2 mt-4">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={page <= 1}
                      onClick={() => setPage((p) => p - 1)}
                    >
                      Previous
                    </Button>
                    <span className="flex items-center text-sm text-muted-foreground px-2">
                      Page {page} of {totalPages}
                    </span>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={page >= totalPages}
                      onClick={() => setPage((p) => p + 1)}
                    >
                      Next
                    </Button>
                  </div>
                )}
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
