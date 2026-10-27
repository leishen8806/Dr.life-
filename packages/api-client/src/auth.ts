export interface TokenPair {
  accessToken: string;
  refreshToken?: string;
}

/** Login implementations live in the apps: Mini App replays the latest initData, Web goes back to the login page. */
export interface AuthProvider {
  login(): Promise<TokenPair>;
  refresh(refreshToken: string): Promise<TokenPair>;
  logout(): Promise<void> | void;
}

export interface StepUpRequest {
  /** 2003 means the gateway asked for verification; the client never re-prompts on 2004. */
  code: 2003;
  requestId: string;
}

/** Returns a step-up token, or null when the user cancels the verification. */
export type StepUpHandler = (request: StepUpRequest) => Promise<string | null>;

export interface TokenStore {
  getAccessToken(): string | null;
  setTokens(tokens: TokenPair): void;
  clear(): void;
  login(): Promise<TokenPair>;
  logout(): Promise<void>;
  /** Single-flight: concurrent callers share one refresh and get the new access token, or null when the session is gone. */
  refreshOnce(): Promise<string | null>;
  onReloginRequired(listener: () => void): () => void;
}

export interface AuthFetchOptions {
  store: TokenStore;
  stepUp?: StepUpHandler;
}

const expiredSession = 2001;
const stepUpRequired = 2003;

/**
 * Tokens live in this closure only. Nothing here touches localStorage, sessionStorage,
 * document.cookie, indexedDB or globalThis, and the store never logs a token value.
 */
export function createTokenStore(provider: AuthProvider): TokenStore {
  let accessToken: string | null = null;
  let refreshToken: string | null = null;
  let inflight: Promise<string | null> | null = null;
  let reloginAnnounced = false;
  const listeners = new Set<() => void>();

  function setTokens(tokens: TokenPair): void {
    accessToken = tokens.accessToken;
    refreshToken = tokens.refreshToken ?? null;
    reloginAnnounced = false;
  }

  function clear(): void {
    accessToken = null;
    refreshToken = null;
  }

  function endSession(): void {
    clear();
    if (reloginAnnounced) return;
    reloginAnnounced = true;
    for (const listener of [...listeners]) listener();
  }

  function refreshOnce(): Promise<string | null> {
    if (inflight) return inflight;
    const token = refreshToken;
    if (!token) {
      endSession();
      return Promise.resolve(null);
    }
    const work = (async (): Promise<string | null> => {
      try {
        const tokens = await provider.refresh(token);
        setTokens(tokens);
        return tokens.accessToken;
      } catch {
        endSession();
        return null;
      }
    })();
    inflight = work;
    void work.finally(() => {
      if (inflight === work) inflight = null;
    });
    return work;
  }

  return {
    getAccessToken: () => accessToken,
    setTokens,
    clear,
    login: async () => {
      const tokens = await provider.login();
      setTokens(tokens);
      return tokens;
    },
    logout: async () => {
      try {
        await provider.logout();
      } finally {
        clear();
      }
    },
    refreshOnce,
    onReloginRequired: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    }
  };
}

interface AuthFailure {
  code: number;
  requestId: string;
}

async function readAuthFailure(response: Response): Promise<AuthFailure | undefined> {
  if (response.status !== 401) return undefined;
  const body: unknown = await response
    .clone()
    .json()
    .catch(() => undefined);
  if (!body || typeof body !== "object") return undefined;
  const envelope = body as { ok?: unknown; error?: { code?: unknown }; request_id?: unknown };
  if (envelope.ok !== false || typeof envelope.request_id !== "string") return undefined;
  const code = envelope.error?.code;
  return typeof code === "number" ? { code, requestId: envelope.request_id } : undefined;
}

/**
 * Innermost fetch layer: it owns the Authorization header so a replay carries the refreshed
 * token, and it sits inside `createResilientFetch` so the idempotency key generated for a
 * write is already on the request and survives the replay unchanged.
 *
 * Each call replays at most once — the step-up JTI is single-use server side, and a second
 * refresh would only loop.
 */
export function createAuthFetch(fetchFn: typeof globalThis.fetch, options: AuthFetchOptions): typeof globalThis.fetch {
  return async (input, init) => {
    const request = new Request(input, init);
    const replayable = request.clone();
    const headers = authorized(new Headers(request.headers), options.store.getAccessToken());
    const response = await fetchFn(new Request(request, { headers }));
    const failed = await readAuthFailure(response);
    if (!failed) return response;

    if (failed.code === expiredSession) {
      const refreshed = await options.store.refreshOnce();
      if (!refreshed) return response;
      return fetchFn(replay(replayable, authorized(new Headers(headers), refreshed), request.signal));
    }

    if (failed.code === stepUpRequired && options.stepUp) {
      const stepUpToken = await options.stepUp({ code: stepUpRequired, requestId: failed.requestId });
      if (!stepUpToken) return response;
      const next = new Headers(headers);
      next.set("X-Step-Up-Token", stepUpToken);
      return fetchFn(replay(replayable, next, request.signal));
    }

    return response;
  };
}

function authorized(headers: Headers, accessToken: string | null): Headers {
  if (accessToken) headers.set("Authorization", `Bearer ${accessToken}`);
  else headers.delete("Authorization");
  return headers;
}

function replay(request: Request, headers: Headers, signal: AbortSignal): Request {
  return new Request(request, { headers, signal });
}
