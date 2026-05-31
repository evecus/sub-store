import { storeToRefs } from "pinia";
import { useMediaQuery } from "@vueuse/core";
import { computed } from "vue";
import { useRoute } from "vue-router";

import { useSettingsStore } from "@/store/settings";
import { SIDEBAR_BREAKPOINT } from "@/store/system";

export const WIDE_SCREEN_NARROW_MODE_BREAKPOINT = SIDEBAR_BREAKPOINT;
export const WIDE_SCREEN_NARROW_MODE_QUERY = `(min-width: ${WIDE_SCREEN_NARROW_MODE_BREAKPOINT}px)`;

export const useWideScreenNarrowMode = () => {
  const route = useRoute();
  const settingsStore = useSettingsStore();

  const { appearanceSetting } = storeToRefs(settingsStore);
  const isWideScreen = useMediaQuery(WIDE_SCREEN_NARROW_MODE_QUERY);

  const supportsWideScreenNarrowMode = computed(
    () => Boolean(route.meta?.needTabBar || route.meta?.hideSideBarInWideScreenNarrowMode)
  );
  const shouldHideSideBarInWideScreenNarrowMode = computed(() => {
    return Boolean(route.meta?.needTabBar || route.meta?.hideSideBarInWideScreenNarrowMode);
  });
  const storedWideScreenNarrowMode = computed(() => {
    return appearanceSetting.value.useNarrowModeOnWideScreen ?? false;
  });

  const isWideScreenNarrowModeActive = computed(() => {
    return supportsWideScreenNarrowMode.value && isWideScreen.value && storedWideScreenNarrowMode.value;
  });

  const showWideScreenNarrowModeToggle = computed(() => {
    return supportsWideScreenNarrowMode.value && isWideScreen.value;
  });

  const shouldShowTabBar = computed(() => {
    return Boolean(route.meta?.needTabBar) && (!isWideScreen.value || isWideScreenNarrowModeActive.value);
  });

  const shouldShowSideBar = computed(() => {
    if (route.path.startsWith("/preview") || !isWideScreen.value) {
      return false;
    }

    if (!shouldHideSideBarInWideScreenNarrowMode.value) {
      return true;
    }

    return !storedWideScreenNarrowMode.value;
  });

  const setWideScreenNarrowMode = async (enabled: boolean) => {
    const nextAppearanceSetting = {
      ...appearanceSetting.value,
      useNarrowModeOnWideScreen: enabled,
    };

    if (enabled && !appearanceSetting.value.listPageViewModeInWideScreenNarrowMode) {
      nextAppearanceSetting.listPageViewModeInWideScreenNarrowMode = appearanceSetting.value.listPageViewMode ?? "dual-column";
    }

    await settingsStore.changeAppearanceSetting({
      appearanceSetting: nextAppearanceSetting,
    });
  };

  const toggleWideScreenNarrowMode = async () => {
    if (!showWideScreenNarrowModeToggle.value) {
      return;
    }

    await setWideScreenNarrowMode(!storedWideScreenNarrowMode.value);
  };

  return {
    isWideScreen,
    supportsWideScreenNarrowMode,
    storedWideScreenNarrowMode,
    isWideScreenNarrowModeActive,
    showWideScreenNarrowModeToggle,
    shouldShowTabBar,
    shouldShowSideBar,
    setWideScreenNarrowMode,
    toggleWideScreenNarrowMode,
  };
};
