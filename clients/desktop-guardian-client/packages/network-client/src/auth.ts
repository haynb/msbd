import { z } from "zod";

export type AuthStatus = "idle" | "authenticating" | "authenticated" | "error";

export interface AuthTokens {
  accessToken: string;
  refreshToken: string;
  expiresAt: number;
  tokenType: string;
  deviceId?: string;
}

const callbackPayloadSchema = z.object({
  access_token: z.string(),
  refresh_token: z.string(),
  expires_in: z.union([z.string(), z.number()]),
  token_type: z.string().default("Bearer"),
  device_id: z.string().optional()
});

const refreshResponseSchema = z.object({
  access_token: z.string(),
  refresh_token: z.string(),
  token_type: z.string().default("Bearer"),
  expires_in: z.number(),
  device_id: z.string().optional()
});

export interface TokenRefreshOptions {
  baseUrl: string;
  refreshToken: string;
  fetchImpl?: typeof fetch;
}

export function tokensFromCallback(callbackUrl: string): AuthTokens {
  const url = new URL(callbackUrl);
  const payload = callbackPayloadSchema.parse({
    access_token: url.searchParams.get("access_token"),
    refresh_token: url.searchParams.get("refresh_token"),
    expires_in: url.searchParams.get("expires_in") ?? "0",
    token_type: url.searchParams.get("token_type") ?? "Bearer",
    device_id: url.searchParams.get("device_id") ?? undefined
  });

  const expiresInMs = typeof payload.expires_in === "string" ? parseInt(payload.expires_in, 10) : payload.expires_in;
  const expiresAt = Date.now() + expiresInMs * 1000;

  return {
    accessToken: payload.access_token,
    refreshToken: payload.refresh_token,
    tokenType: payload.token_type,
    deviceId: payload.device_id,
    expiresAt
  };
}

export async function refreshTokens(options: TokenRefreshOptions): Promise<AuthTokens> {
  const { baseUrl, refreshToken, fetchImpl = fetch } = options;
  const response = await fetchImpl(new URL("/api/auth/refresh", baseUrl).toString(), {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${refreshToken}`
    },
    body: JSON.stringify({ grant_type: "refresh_token" })
  });

  if (!response.ok) {
    throw new Error(`Failed to refresh token: ${response.status}`);
  }

  const json = await response.json();
  const payload = refreshResponseSchema.parse(json);

  return {
    accessToken: payload.access_token,
    refreshToken: payload.refresh_token,
    tokenType: payload.token_type,
    deviceId: payload.device_id,
    expiresAt: Date.now() + payload.expires_in * 1000
  };
}
