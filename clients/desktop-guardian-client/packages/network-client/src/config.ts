import { z } from "zod";

export const remoteConfigSchema = z.object({
  version: z.string(),
  policies: z.record(z.any()).default({}),
  audio: z.object({
    sampleRate: z.number().default(16000),
    noiseGate: z.number().default(-45),
    gain: z.number().default(0)
  }).default({ sampleRate: 16000, noiseGate: -45, gain: 0 }),
  guardian: z.object({
    enabled: z.boolean().default(true),
    aggressive: z.boolean().default(false)
  }).default({ enabled: true, aggressive: false })
});

export type RemoteConfig = z.infer<typeof remoteConfigSchema>;

export interface FetchConfigOptions {
  baseUrl: string;
  token: string;
  fetchImpl?: typeof fetch;
}

export async function fetchRemoteConfig(options: FetchConfigOptions): Promise<RemoteConfig> {
  const { baseUrl, token, fetchImpl = fetch } = options;
  const response = await fetchImpl(new URL("/api/device/config", baseUrl).toString(), {
    method: "GET",
    headers: {
      Authorization: `Bearer ${token}`
    }
  });

  if (!response.ok) {
    throw new Error(`Failed to fetch config: ${response.status}`);
  }

  const payload = await response.json();
  return remoteConfigSchema.parse(payload);
}

export interface ConfigSubscription {
  close: () => void;
}

export interface SubscribeConfigOptions {
  baseUrl: string;
  token: string;
  onConfig: (config: RemoteConfig) => void;
  pollIntervalMs?: number;
  fetchImpl?: typeof fetch;
}

export function subscribeToConfig(options: SubscribeConfigOptions): ConfigSubscription {
  const { baseUrl, token, onConfig, pollIntervalMs = 5000, fetchImpl } = options;
  let closed = false;
  let timer: NodeJS.Timeout | undefined;

  const tick = async () => {
    if (closed) return;

    try {
      const config = await fetchRemoteConfig({ baseUrl, token, fetchImpl });
      onConfig(config);
    } catch (error) {
      console.error("config poll error", error);
    } finally {
      if (!closed) {
        timer = setTimeout(tick, pollIntervalMs);
      }
    }
  };

  void tick();

  return {
    close: () => {
      closed = true;
      if (timer) {
        clearTimeout(timer);
      }
    }
  };
}
