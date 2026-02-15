(() => {
  const toast = document.getElementById("toast");

  const showToast = (message, type = "success") => {
    if (!toast) return;
    toast.className = `toast ${type}`;
    toast.textContent = message;
    toast.classList.remove("hidden");
    setTimeout(() => toast.classList.add("hidden"), 3500);
  };

  const setupDropzone = () => {
    const form = document.getElementById("uploadForm");
    const dropzone = document.getElementById("dropzone");
    const input = document.getElementById("video");

    if (!form || !dropzone || !input) return;

    const setFile = (file) => {
      const dt = new DataTransfer();
      dt.items.add(file);
      input.files = dt.files;
      dropzone.querySelector("p").textContent = file.name;
      showToast("Arquivo selecionado", "success");
    };

    dropzone.addEventListener("click", () => input.click());
    input.addEventListener("change", () => {
      if (input.files && input.files[0]) setFile(input.files[0]);
    });

    ["dragenter", "dragover"].forEach((eventName) => {
      dropzone.addEventListener(eventName, (event) => {
        event.preventDefault();
        dropzone.classList.add("active");
      });
    });

    ["dragleave", "drop"].forEach((eventName) => {
      dropzone.addEventListener(eventName, (event) => {
        event.preventDefault();
        dropzone.classList.remove("active");
      });
    });

    dropzone.addEventListener("drop", (event) => {
      const file = event.dataTransfer?.files?.[0];
      if (!file) return;
      setFile(file);
    });

    form.addEventListener("submit", () => {
      showToast("Upload iniciado...", "success");
    });
  };

  const setupJobPage = () => {
    const jobCard = document.getElementById("job-card");
    if (!jobCard) return;

    const jobID = jobCard.dataset.jobId;
    const progressBar = document.getElementById("progress-bar");
    const progressText = document.getElementById("progress-text");
    const progressValue = document.getElementById("progress-value");
    const resultSlot = document.getElementById("result-slot");

    let autoDownloaded = false;

    const updateProgress = (value, text) => {
      const clamped = Math.max(0, Math.min(value || 0, 100));
      if (progressBar) progressBar.style.width = `${clamped}%`;
      if (progressValue) progressValue.textContent = `${clamped}%`;
      if (progressText && text) progressText.textContent = text;
    };

    const showTranscriptionPendingCard = () => {
      if (!resultSlot) return;
      if (document.getElementById("transcription-pending-card")) return;
      resultSlot.innerHTML += `
        <div id="transcription-pending-card" class="rounded-xl border border-cyan-500/30 bg-cyan-500/10 p-4 mt-3">
          <p class="font-semibold text-cyan-300">Transcrição em andamento</p>
          <p class="text-sm text-slate-300 mt-1">Processando áudio localmente com whisper.cpp...</p>
        </div>
      `;
    };

    const startTranscription = async (button) => {
      if (button) button.disabled = true;
      if (button) button.textContent = "Transcrevendo...";
      showTranscriptionPendingCard();
      updateProgress(2, "Transcrição em fila...");
      try {
        const response = await fetch(`/transcribe/${jobID}`, { method: "GET" });
        if (!response.ok) {
          const message = await response.text();
          showToast(message || "Falha ao iniciar transcrição", "error");
          if (button) {
            button.disabled = false;
            button.textContent = "Transcrever em texto";
          }
          return;
        }
        showToast("Transcrição iniciada", "success");
      } catch {
        showToast("Não foi possível iniciar a transcrição", "error");
        if (button) {
          button.disabled = false;
          button.textContent = "Transcrever em texto";
        }
      }
    };

    const renderExtractionActions = (downloadURL) => {
      if (!resultSlot || !downloadURL) return;
      resultSlot.innerHTML = `
        <div class="rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-4 flex flex-col md:flex-row gap-4 md:items-center md:justify-between">
          <div>
            <p class="font-semibold text-emerald-300">Áudio pronto</p>
            <p class="text-sm text-slate-300 mt-1">Baixe o áudio ou gere a transcrição local.</p>
          </div>
          <div class="flex gap-2">
            <a class="btn-success" href="${downloadURL}">Baixar áudio</a>
            <button id="transcribe-btn" type="button" class="btn-secondary">Transcrever em texto</button>
          </div>
        </div>
      `;

      if (!autoDownloaded) {
        autoDownloaded = true;
        const link = document.createElement("a");
        link.href = downloadURL;
        link.style.display = "none";
        document.body.appendChild(link);
        link.click();
        link.remove();
      }

      const transcribeBtn = document.getElementById("transcribe-btn");
      if (transcribeBtn) {
        transcribeBtn.addEventListener("click", () => startTranscription(transcribeBtn));
      }
    };

    const renderTranscriptActions = (txtURL, srtURL) => {
      if (!resultSlot || (!txtURL && !srtURL)) return;
      if (document.getElementById("transcript-ready-card")) return;
      const pending = document.getElementById("transcription-pending-card");
      if (pending) pending.remove();
      resultSlot.innerHTML += `
        <div id="transcript-ready-card" class="rounded-xl border border-cyan-500/30 bg-cyan-500/10 p-4 mt-3">
          <p class="font-semibold text-cyan-300">Transcrição concluída</p>
          <p class="text-sm text-slate-300 mt-1 mb-3">Baixe o conteúdo da transcrição no formato desejado.</p>
          <div class="flex gap-2">
            ${txtURL ? `<a class="btn-secondary" href="${txtURL}">Baixar TXT</a>` : ""}
            ${srtURL ? `<a class="btn-secondary" href="${srtURL}">Baixar SRT</a>` : ""}
          </div>
        </div>
      `;
    };

    const applyJobSnapshot = (data) => {
      const extractionStatus = data.status;
      const extractionProgress = Number(data.progress || 0);
      const transcriptStatus = data.transcript_status;
      const transcriptProgress = Number(data.transcript_progress || 0);

      if (extractionStatus === "failed") {
        updateProgress(0, data.error || "Falha na extração");
        return;
      }

      if (extractionStatus === "completed") {
        updateProgress(100, "Extração concluída");
        renderExtractionActions(data.download_url);
      } else if (extractionStatus === "processing" || extractionStatus === "queued") {
        updateProgress(extractionProgress, "Extraindo áudio...");
      }

      if (transcriptStatus === "queued" || transcriptStatus === "processing") {
        showTranscriptionPendingCard();
        updateProgress(transcriptProgress, "Transcrevendo áudio...");
      }

      if (transcriptStatus === "failed") {
        updateProgress(0, data.transcript_error || "Falha na transcrição");
      }

      if (transcriptStatus === "completed") {
        updateProgress(100, "Transcrição concluída");
        renderTranscriptActions(data.transcript_txt_url, data.transcript_srt_url);
      }
    };

    const startPolling = () => {
      const tick = async () => {
        try {
          const res = await fetch(`/api/job/${jobID}`, { cache: "no-store" });
          if (!res.ok) return;
          const data = await res.json();
          applyJobSnapshot(data);
        } catch {
          // fallback silencioso; websocket segue tentando entregar eventos.
        }
      };

      tick();
      return setInterval(tick, 2000);
    };

    fetch(`/extract/${jobID}`, { method: "GET" }).catch(() => {
      showToast("Não foi possível iniciar a extração", "error");
    });

    const pollTimer = startPolling();

    const protocol = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${protocol}://${location.host}/ws/${jobID}`);

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        const stage = data.stage || "extraction";

        if (stage === "transcription") {
          updateProgress(data.progress, data.message || "Transcrevendo áudio...");

          if (data.status === "failed") {
            showToast(data.error || "Falha na transcrição", "error");
            if (progressText) progressText.textContent = data.error || "Falha na transcrição";
            return;
          }

          if (data.status === "completed") {
            updateProgress(100, "Transcrição concluída");
            renderTranscriptActions(data.transcript_txt_url, data.transcript_srt_url);
            showToast("Transcrição concluída", "success");
          }
          return;
        }

        updateProgress(data.progress, data.message || data.status || "Processando...");

        if (data.status === "failed") {
          showToast(data.error || "Falha na extração", "error");
          if (progressText) progressText.textContent = data.error || "Falha na extração";
          return;
        }

        if (data.status === "completed") {
          updateProgress(100, "Extração concluída");
          renderExtractionActions(data.download_url);
          showToast("Extração concluída", "success");
        }
      } catch {
        showToast("Mensagem de progresso inválida", "error");
      }
    };

    ws.onerror = () => {
      showToast("Conexão de progresso perdida", "error");
    };

    ws.onclose = () => {
      showToast("Canal de progresso encerrado, usando atualização automática", "error");
    };

    window.addEventListener("beforeunload", () => clearInterval(pollTimer));
  };

  setupDropzone();
  setupJobPage();
})();
