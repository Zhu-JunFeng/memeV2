import { mount } from "@vue/test-utils";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

vi.mock("element-plus", () => ({
  ElMessage: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { ElMessage } from "element-plus";
import TokenAddressLink from "./TokenAddressLink.vue";

describe("TokenAddressLink", () => {
  const originalClipboard = navigator.clipboard;
  const originalExecCommand = document.execCommand;
  const originalSecureContext = window.isSecureContext;

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: originalClipboard,
    });
    document.execCommand = originalExecCommand;
    Object.defineProperty(window, "isSecureContext", {
      configurable: true,
      value: originalSecureContext,
    });
  });

  it("uses clipboard api in secure context", async () => {
    const writeText = vi.fn().mockResolvedValue();
    Object.defineProperty(window, "isSecureContext", {
      configurable: true,
      value: true,
    });
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    const wrapper = mount(TokenAddressLink, {
      props: { address: "So11111111111111111111111111111111111111112" },
      global: {
        stubs: {
          "el-button": {
            template: "<button @click=\"$emit('click', $event)\"><slot /></button>",
          },
        },
      },
    });

    await wrapper.findAll("button")[1].trigger("click");

    expect(writeText).toHaveBeenCalledWith(
      "So11111111111111111111111111111111111111112",
    );
    expect(ElMessage.success).toHaveBeenCalledWith("CA 已复制");
    expect(ElMessage.error).not.toHaveBeenCalled();
  });

  it("falls back to execCommand on insecure context", async () => {
    Object.defineProperty(window, "isSecureContext", {
      configurable: true,
      value: false,
    });
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: undefined,
    });
    document.execCommand = vi.fn().mockReturnValue(true);

    const wrapper = mount(TokenAddressLink, {
      props: { address: "So11111111111111111111111111111111111111112" },
      global: {
        stubs: {
          "el-button": {
            template: "<button @click=\"$emit('click', $event)\"><slot /></button>",
          },
        },
      },
    });

    await wrapper.findAll("button")[1].trigger("click");

    expect(document.execCommand).toHaveBeenCalledWith("copy");
    expect(ElMessage.success).toHaveBeenCalledWith("CA 已复制");
    expect(ElMessage.error).not.toHaveBeenCalled();
  });
});
