<template>
  <div class="app-shell">
    <nav class="app-nav" aria-label="主导航">
      <router-link to="/">压力突破回测</router-link>
      <router-link to="/bollinger-pressure">BOLL 压力带</router-link>
      <router-link to="/horizontal-pressure">水平压力位</router-link>
    </nav>
    <router-view />
  </div>
</template>
