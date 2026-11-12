import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/theme.css'
import './styles/ui.css'
import './styles/app.css'
import './styles/session.css'
import './styles/pages.css'
import App from './App.tsx'
import DialogProvider from './dialogs/DialogProvider.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <DialogProvider>
      <App />
    </DialogProvider>
  </StrictMode>,
)
