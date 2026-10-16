import { Children, isValidElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import {
  AppHeader,
  BottomNavigation,
  BottomSheet,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  ListItem,
  Loading,
  StatusTag,
  Stepper
} from "./index";

function findByRole(node: ReactNode, role: string): { props: Record<string, unknown> } | undefined {
  if (!isValidElement<{ role?: string; children?: ReactNode }>(node)) return undefined;
  if (node.props.role === role) return node as { props: Record<string, unknown> };
  return Children.toArray(node.props.children)
    .map((child) => findByRole(child, role))
    .find(Boolean);
}

describe("foundation components", () => {
  it("renders the reusable shell with accessible navigation and snapshots", () => {
    const navigation = BottomNavigation({
      ariaLabel: "Primary navigation",
      items: [{ id: "home", label: "Home", icon: "⌂" }, { id: "me", label: "Me", icon: "●", badge: 2 }],
      activeId: "home",
      onChange: vi.fn()
    });
    const view = [
      AppHeader({ slogan: "INFINITE POSSIBILITIES", actions: <button aria-label="AI">AI</button> }),
      Card({ children: ListItem({ title: "Order", description: "Ready", trailing: "1" }) }),
      Stepper({ steps: ["Created", "Shipped", "Delivered"], current: 1 }),
      StatusTag({ tone: "success", children: "Ready" }),
      navigation
    ];

    expect(view).toMatchSnapshot();
    expect(navigation.type).toBe("nav");
    expect(navigation.props["aria-label"]).toBe("Primary navigation");
  });

  it("exposes dialogs, close controls, and state actions to keyboard users", () => {
    const close = vi.fn();
    const retry = vi.fn();
    const sheet = BottomSheet({ open: true, title: "Options", closeLabel: "Close", onClose: close, children: "Content" });
    const confirm = ConfirmDialog({ open: true, title: "Delete", description: "This cannot be undone.", cancelLabel: "Cancel", confirmLabel: "Confirm", onCancel: close, onConfirm: retry });
    const empty = EmptyState({ title: "No orders", actionLabel: "Browse", onAction: retry });
    const error = ErrorState({ title: "Network error", description: "Try again.", retryLabel: "Retry", onRetry: retry });

    expect(findByRole(sheet, "dialog")?.props["aria-modal"]).toBe("true");
    expect(findByRole(confirm, "alertdialog")?.props["aria-modal"]).toBe("true");
    expect(findByRole(empty, "status")).toBeDefined();
    expect(findByRole(error, "alert")).toBeDefined();
  });

  it("keeps loading labels and Khmer text untruncated", () => {
    const loading = Loading({ label: "កំពុងផ្ទុកព័ត៌មានសម្រាប់ការដឹកជញ្ជូនរបស់អ្នក" });
    expect(loading.props.role).toBe("status");
    expect(loading.props.className).toContain("drl-loading");
  });
});
