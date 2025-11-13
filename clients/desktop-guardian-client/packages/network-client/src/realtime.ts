import path from "node:path";
import { randomUUID } from "node:crypto";
import * as grpc from "@grpc/grpc-js";
import protoLoader from "@grpc/proto-loader";

export interface ParticipantInput {
  userId: string;
  role?: string;
}

export interface CreateSessionOptions {
  token: string;
  mode?: string;
  requestKey?: string;
  deviceId?: string;
  metadata?: Record<string, string>;
  participants?: ParticipantInput[];
}

export interface TransitionSessionOptions {
  token: string;
  sessionId: string;
  nextState: "active" | "paused" | "ended";
  metadataPatch?: Record<string, string>;
}

export interface SessionRecord {
  id: string;
  tenantId: string;
  userId: string;
  state: string;
  mode: string;
  requestKey: string;
  metadata: Record<string, string>;
  startedAt?: string;
  updatedAt?: string;
  endedAt?: string;
}

export interface ControlClientOptions {
  target: string;
  useTls?: boolean;
}

interface CreateSessionRequestMessage {
  mode?: string;
  requestKey?: string;
  deviceId?: string;
  metadata?: Record<string, string>;
  participants?: Array<{ userId?: string; role?: string }>;
}

interface TransitionSessionRequestMessage {
  sessionId: string;
  nextState: string;
  metadataPatch?: Record<string, string>;
}

interface SessionRecordMessage {
  id?: string;
  tenantId?: string;
  userId?: string;
  state?: string;
  mode?: string;
  requestKey?: string;
  metadata?: Record<string, string>;
  startedAt?: string;
  updatedAt?: string;
  endedAt?: string;
}

interface ControlServiceClient extends grpc.Client {
  createSession(
    request: CreateSessionRequestMessage,
    metadata: grpc.Metadata,
    callback: (err: grpc.ServiceError | null, response?: SessionRecordMessage) => void
  ): void;
  transitionSession(
    request: TransitionSessionRequestMessage,
    metadata: grpc.Metadata,
    callback: (err: grpc.ServiceError | null, response?: SessionRecordMessage) => void
  ): void;
}

interface ProtoGrpcType {
  realtime: {
    v1: {
      ControlService: grpc.ServiceClientConstructor;
    };
  };
}

const loaderOptions: protoLoader.Options = {
  keepCase: false,
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true
};

const moduleDir = typeof __dirname === "string" ? __dirname : path.resolve(process.cwd(), "packages/network-client/src");
const protoDir = path.resolve(moduleDir, "../../../../../", "services/realtime-interview-core/proto/realtime/v1");
const controlProtoPath = path.join(protoDir, "control.proto");

let controlCtor: (new (address: string, credentials: grpc.ChannelCredentials, options?: object) => ControlServiceClient) | null = null;

function loadControlCtor() {
  if (controlCtor) {
    return controlCtor;
  }
  const packageDefinition = protoLoader.loadSync(controlProtoPath, loaderOptions);
  const descriptor = grpc.loadPackageDefinition(packageDefinition) as unknown as ProtoGrpcType;
  const ctor = descriptor.realtime.v1.ControlService as grpc.ServiceClientConstructor;
  controlCtor = ctor as unknown as new (
    address: string,
    credentials: grpc.ChannelCredentials,
    options?: object
  ) => ControlServiceClient;
  return controlCtor;
}

function normalizeTarget(raw: string): string {
  if (!raw) {
    throw new Error("target is required");
  }
  if (raw.startsWith("dns://") || raw.startsWith("unix://")) {
    return raw;
  }
  if (/^https?:\/\//i.test(raw)) {
    return raw;
  }
  if (raw.includes("/")) {
    return `dns:///${raw}`;
  }
  return raw;
}

function buildMetadata(token: string) {
  const metadata = new grpc.Metadata();
  metadata.set("authorization", `Bearer ${token}`);
  return metadata;
}

function mapSessionRecord(record?: SessionRecordMessage): SessionRecord {
  if (!record || !record.id) {
    throw new Error("invalid session response");
  }
  return {
    id: record.id,
    tenantId: record.tenantId ?? "",
    userId: record.userId ?? "",
    state: record.state ?? "",
    mode: record.mode ?? "",
    requestKey: record.requestKey ?? "",
    metadata: record.metadata ?? {},
    startedAt: record.startedAt,
    updatedAt: record.updatedAt,
    endedAt: record.endedAt
  };
}

export class RealtimeControlClient {
  private readonly client: ControlServiceClient;

  constructor(options: ControlClientOptions) {
    const target = normalizeTarget(options.target);
    const creds = options.useTls ? grpc.credentials.createSsl() : grpc.credentials.createInsecure();
    const Ctor = loadControlCtor();
    this.client = new Ctor(target, creds, {
      "grpc.max_receive_message_length": 5 * 1024 * 1024
    });
  }

  async createSession(options: CreateSessionOptions): Promise<SessionRecord> {
    const requestKey = options.requestKey ?? randomUUID();
    const payload: CreateSessionRequestMessage = {
      mode: options.mode ?? "interview",
      requestKey,
      deviceId: options.deviceId,
      metadata: options.metadata,
      participants: options.participants?.map((participant) => ({
        userId: participant.userId,
        role: participant.role
      }))
    };
    const metadata = buildMetadata(options.token);
    return new Promise<SessionRecord>((resolve, reject) => {
      this.client.createSession(payload, metadata, (err, response) => {
        if (err) {
          reject(new Error(`createSession failed: ${err.message}`));
          return;
        }
        try {
          resolve(mapSessionRecord(response));
        } catch (mapErr) {
          reject(mapErr);
        }
      });
    });
  }

  async transitionSession(options: TransitionSessionOptions): Promise<SessionRecord> {
    const payload: TransitionSessionRequestMessage = {
      sessionId: options.sessionId,
      nextState: options.nextState,
      metadataPatch: options.metadataPatch
    };
    const metadata = buildMetadata(options.token);
    return new Promise<SessionRecord>((resolve, reject) => {
      this.client.transitionSession(payload, metadata, (err, response) => {
        if (err) {
          reject(new Error(`transitionSession failed: ${err.message}`));
          return;
        }
        try {
          resolve(mapSessionRecord(response));
        } catch (mapErr) {
          reject(mapErr);
        }
      });
    });
  }

  close() {
    this.client.close();
  }
}

export function createRealtimeControlClient(options: ControlClientOptions) {
  return new RealtimeControlClient(options);
}
