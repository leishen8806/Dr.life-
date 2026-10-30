import type { components } from "./schema";
import {
  createApiClient,
  createTokenStore,
  withIdempotencyKey,
  type ApiClientOptions,
  type AuthProvider,
  type Language,
  type TokenStore
} from "./index";

type ApiClient = ReturnType<typeof createApiClient>;
type AuthData = components["schemas"]["TelegramAuthData"];
type ProfileData = components["schemas"]["AccountProfileData"];

/** The Mini App ends that sign in with Telegram initData; the role is part of the path. */
export type SessionEnd = "buyer" | "merchant" | "driver";

/** The account fields the startup flow needs, normalised across login and profile responses. */
export interface SessionAccount {
  publicId: string;
  role: SessionEnd;
  lang: Language | null;
  status: "active" | "frozen" | "disabled";
  needsLanguage: boolean;
}

export interface TelegramSessionOptions
  extends Pick<ApiClientOptions, "baseUrl" | "fetch" | "getLanguage"> {
  end: SessionEnd;
  /** Read on every login so a replay never sends a stale initData copy. */
  initData: () => string;
  deviceId?: string;
}

export interface TelegramSession {
  /** Authenticated client for everything after login; it owns the memory-only token store. */
  client: ApiClient;
  store: TokenStore;
  /** Replays initData against this end's login endpoint and returns the account it belongs to. */
  login(): Promise<SessionAccount>;
  /** First-time language registration: the server stores one language per account. */
  registerLanguage(language: Language): Promise<SessionAccount>;
}

/** A success envelope is mandatory; a missing one means something other than our API answered. */
function required<T>(data: T | undefined): T {
  if (data === undefined) throw new Error("Session request returned no payload");
  return data;
}

function fromAuth(data: AuthData): SessionAccount {
  return {
    publicId: data.account.public_id,
    role: data.account.role,
    lang: data.account.lang ?? null,
    status: data.account.status,
    needsLanguage: data.needs_language
  };
}

function fromProfile(data: ProfileData): SessionAccount {
  return {
    publicId: data.public_id,
    role: data.role,
    lang: data.lang ?? null,
    status: data.status,
    needsLanguage: data.needs_language
  };
}

/** The path carries the role, so each end gets its own typed call rather than a built string. */
function putLanguage(client: ApiClient, end: SessionEnd, lang: Language) {
  const params = { header: withIdempotencyKey() };
  if (end === "buyer")
    return client.PUT("/v1/buyer/me/language", { params, body: { lang } });
  if (end === "merchant")
    return client.PUT("/v1/merchant/me/language", { params, body: { lang } });
  return client.PUT("/v1/driver/me/language", { params, body: { lang } });
}

function postLogout(client: ApiClient, end: SessionEnd) {
  const params = { header: withIdempotencyKey() };
  if (end === "buyer") return client.POST("/v1/buyer/auth/logout", { params });
  if (end === "merchant")
    return client.POST("/v1/merchant/auth/logout", { params });
  return client.POST("/v1/driver/auth/logout", { params });
}

/**
 * Wires one Mini App end to its login, refresh and language endpoints.
 *
 * Login and refresh go through a second, unauthenticated client: sending an expired
 * bearer token to a `security: []` path only earns an extra rejection, and the
 * authenticated client would re-enter its own refresh path while refreshing.
 */
export function createTelegramSession(
  options: TelegramSessionOptions
): TelegramSession {
  const shared: ApiClientOptions = {
    baseUrl: options.baseUrl,
    fetch: options.fetch,
    getLanguage: options.getLanguage
  };
  // No retry on login or refresh. `createResilientFetch` stamps every write with an
  // idempotency key, which would otherwise make a 429 retryable: the 2007 lockout would
  // be slept through and retried away, and the countdown would never reach the user.
  const publicClient = createApiClient({
    ...shared,
    resilience: { retry: { maxAttempts: 1 } }
  });
  let account: SessionAccount | undefined;

  const provider: AuthProvider = {
    async login() {
      const { data } = await publicClient.POST("/v1/public/auth/telegram", {
        body: {
          end: options.end,
          init_data: options.initData(),
          device_id: options.deviceId
        }
      });
      const payload = required(data).data;
      account = fromAuth(payload);
      return {
        accessToken: payload.access_token,
        refreshToken: payload.refresh_token
      };
    },
    async refresh(refreshToken) {
      const { data } = await publicClient.POST("/v1/public/auth/refresh", {
        body: { refresh_token: refreshToken }
      });
      const payload = required(data).data;
      return {
        accessToken: payload.access_token,
        refreshToken: payload.refresh_token
      };
    },
    async logout() {
      await postLogout(client, options.end);
    }
  };

  const store = createTokenStore(provider);
  const client = createApiClient({ ...shared, auth: { store } });

  return {
    client,
    store,
    async login() {
      await store.login();
      return required(account);
    },
    async registerLanguage(language) {
      const { data } = await putLanguage(client, options.end, language);
      account = fromProfile(required(data).data);
      return account;
    }
  };
}
