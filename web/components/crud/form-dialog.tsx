"use client";

import type { ReactNode } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

interface FormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Optional trigger node (a `<DialogTrigger>` element). */
  trigger?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  /** Footer content (usually the submit button). */
  footer?: ReactNode;
  contentClassName?: string;
  children: ReactNode;
}

/**
 * Shared create/edit dialog skeleton: header (title + description), body, and
 * footer, so CRUD pages only supply their form fields and submit button.
 */
export function FormDialog({
  open,
  onOpenChange,
  trigger,
  title,
  description,
  footer,
  contentClassName = "sm:max-w-md",
  children,
}: FormDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {trigger}
      <DialogContent className={contentClassName}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        {children}
        {footer && <DialogFooter>{footer}</DialogFooter>}
      </DialogContent>
    </Dialog>
  );
}
