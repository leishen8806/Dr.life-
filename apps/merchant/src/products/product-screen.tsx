import { useCallback, useEffect, useState } from "react";
import { useI18n, type TranslationKey } from "@drlife/i18n";
import {
  AppHeader,
  ConfirmDialog,
  ErrorState,
  LanguageSwitcher,
  Loading
} from "@drlife/ui";
import type { MerchantProductApi } from "./api";
import { productErrorKey, type ProductAction } from "./errors";
import {
  checkProductDraft,
  draftFromProduct,
  emptyProductDraft,
  toUpdateRequest,
  unpublishPatch,
  type Category,
  type Product,
  type ProductDraft,
  type ProductFieldError
} from "./form";
import { ProductFormView, ProductListView } from "./product-views";

interface Editing {
  /** 缺省表示新建；带上商品表示在编辑它。 */
  product?: Product;
  draft: ProductDraft;
}

export function ProductScreen({ api }: { api: MerchantProductApi }) {
  const { language, t } = useI18n();
  const [products, setProducts] = useState<Product[]>();
  const [categories, setCategories] = useState<Category[]>([]);
  const [editing, setEditing] = useState<Editing>();
  const [errors, setErrors] = useState<ProductFieldError[]>([]);
  const [failure, setFailure] = useState<TranslationKey>();
  const [unpublishing, setUnpublishing] = useState<Product>();
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => {
    try {
      const [items, enabled] = await Promise.all([
        api.list("all"),
        api.listCategories()
      ]);
      setProducts(items);
      setCategories(enabled);
      setFailure(undefined);
    } catch (error) {
      setFailure(productErrorKey(error, "load"));
    }
  }, [api]);

  useEffect(() => {
    // 包一层 async 是为了让 react-hooks/set-state-in-effect 看见 await 边界：
    // 直接 void reload() 会被判成在 effect 里同步 setState。
    async function load() {
      await reload();
    }
    void load();
  }, [reload]);

  const run = async (action: ProductAction, work: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await work();
      setFailure(undefined);
      setEditing(undefined);
      setErrors([]);
      await reload();
    } catch (error) {
      setFailure(productErrorKey(error, action));
    } finally {
      setBusy(false);
    }
  };

  const save = () => {
    if (!editing) return;
    const { errors: found, request } = checkProductDraft(editing.draft);
    setErrors(found);
    // 校验没过就不发请求，省掉一次必定被 1002 拒绝的往返。
    if (!request) return;
    const existing = editing.product;
    void run("save", () =>
      existing
        ? api.update(existing.public_id, toUpdateRequest(request))
        : api.create(request)
    );
  };

  const closeForm = () => {
    setEditing(undefined);
    setErrors([]);
  };

  const startCreating = () => setEditing({ draft: emptyProductDraft() });

  return (
    <main className="mini-shell">
      <AppHeader slogan={t("brand.slogan")} actions={<LanguageSwitcher />} />
      <h1>{t("product.list.title")}</h1>
      {failure && (
        <ErrorState
          title={t("common.failure")}
          description={t(failure)}
          retryLabel={t("common.retry")}
          onRetry={() => void reload()}
        />
      )}
      {products === undefined ? (
        <Loading label={t("common.loading")} skeleton />
      ) : editing ? (
        <ProductFormView
          mode={editing.product ? "edit" : "create"}
          draft={editing.draft}
          errors={errors}
          categories={categories}
          language={language}
          translate={t}
          busy={busy}
          onChange={(draft) => setEditing({ ...editing, draft })}
          onSubmit={save}
          onCancel={closeForm}
        />
      ) : (
        <>
          <button
            type="button"
            className="merchant-create"
            onClick={startCreating}
          >
            {t("product.list.create")}
          </button>
          <ProductListView
            products={products}
            language={language}
            translate={t}
            onCreate={startCreating}
            onEdit={(product) =>
              setEditing({ product, draft: draftFromProduct(product) })
            }
            onPublish={(product) =>
              void run("publish", () => api.publish(product.public_id))
            }
            onUnpublish={setUnpublishing}
          />
        </>
      )}
      <ConfirmDialog
        open={unpublishing !== undefined}
        title={t("product.unpublish.title")}
        description={t("product.unpublish.description")}
        cancelLabel={t("common.cancel")}
        confirmLabel={t("product.action.unpublish")}
        onCancel={() => setUnpublishing(undefined)}
        onConfirm={() => {
          const product = unpublishing;
          if (!product) return;
          setUnpublishing(undefined);
          void run("unpublish", () =>
            api.update(product.public_id, unpublishPatch())
          );
        }}
      />
    </main>
  );
}
