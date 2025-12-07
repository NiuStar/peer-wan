import React, { useState } from 'react';
import Login from './pages/Login.jsx';
import Register from './pages/Register.jsx';
import Admin from './pages/Admin.jsx';
import { storage } from './api';

export default function App() {
  const initialView = storage.token ? 'app' : (storage.adminSet ? 'login' : 'register');
  const [view, setView] = useState(initialView);

  if (view === 'login') return <Login onAuthed={() => setView('app')} onSwitch={() => setView('register')} />;
  if (view === 'register') return <Register onAuthed={() => setView('app')} onSwitch={() => setView('login')} />;
  return <Admin />;
}
