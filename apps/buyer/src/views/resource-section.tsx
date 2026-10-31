import type { ReactNode } from "react";
import { EmptyState, ErrorState, Loading } from "@drlife/ui";
import type { TranslationKey } from "@drlife/i18n";
import type { Resource } from "../resource";
import type { Translate } from "./common";

/** Renders the loading, empty and error states of one panel in one place. */
export function ResourceSection<T>({
  resource,
  t,
  emptyKey,
  isEmpty,
  onRetry,
  children
}: {
  resource: Resource<T>;
  t: Translate;
  emptyKey: TranslationKey;
  isEmpty: (value: T) => boolean;
  onRetry: () => void;
  children: (value: T) => ReactNode;
}) {
  if (resource.status === "loading")
    return <Loading label={t("common.loading")} skeleton />;
  if (resource.status === "error")
    return (
      <ErrorState
        title={t("mall.loadError")}
        description={t(resource.messageKey)}
        retryLabel={t("common.retry")}
        onRetry={onRetry}
      />
    );
  if (isEmpty(resource.value))
    return (
      <EmptyState
        title={t(emptyKey)}
        actionLabel={t("common.refresh")}
        onAction={onRetry}
      />
    );
  return <>{children(resource.value)}</>;
}

export function isEmptyList(value: readonly unknown[]): boolean {
  return value.length === 0;
}
