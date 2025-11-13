import "@testing-library/jest-dom/vitest";
import { getGuardianWindowController, type GuardianWindowController } from "./modules/test-helpers/guardianStub";

declare global {
  // eslint-disable-next-line no-var
  var guardianTestController: GuardianWindowController;
}

const controller = getGuardianWindowController();
(globalThis as { guardianTestController: GuardianWindowController }).guardianTestController = controller;
