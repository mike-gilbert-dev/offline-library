import { mount } from 'svelte';
import '@fontsource-variable/geist';
import './app.css';
import App from './App.svelte';

// Follow the operating system's light/dark setting (shadcn themes key off .dark).
const darkQuery = window.matchMedia('(prefers-color-scheme: dark)');
const applyTheme = () => document.documentElement.classList.toggle('dark', darkQuery.matches);
applyTheme();
darkQuery.addEventListener('change', applyTheme);

mount(App, { target: document.getElementById('app')! });
