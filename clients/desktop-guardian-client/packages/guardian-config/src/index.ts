import { z } from "zod";

export const guardianConfigSchema = z.object({
  apiBaseUrl: z.string().url().describe("cloud-access-core base URL"),
  realtimeGatewayUrl: z.string().url().describe("realtime-interview-core WS/gRPC entry"),
  defaultRegion: z.string().default("cn-shanghai"),
  updateIntervalSeconds: z.number().int().min(5).max(600).default(30),
  sentryDsn: z.string().optional(),
  flags: z.object({
    enableGuardianPreview: z.boolean().default(false),
    enableNativeAudio: z.boolean().default(true)
  }).default({ enableGuardianPreview: false, enableNativeAudio: true })
});

export type GuardianConfig = z.infer<typeof guardianConfigSchema>;

export const defaultGuardianConfig: GuardianConfig = guardianConfigSchema.parse({
  apiBaseUrl: "https://auth.local",
  realtimeGatewayUrl: "https://realtime.local",
  defaultRegion: "cn-shanghai",
  updateIntervalSeconds: 30,
  flags: {
    enableGuardianPreview: false,
    enableNativeAudio: true
  }
});
