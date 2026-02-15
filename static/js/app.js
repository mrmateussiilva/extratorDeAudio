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

    const updateProgress = (value, text) => {
      const clamped = Math.max(0, Math.min(value || 0, 100));
      if (progressBar) progressBar.style.width = `${clamped}%`;
      if (progressValue) progressValue.textContent = `${clamped}%`;
      if (progressText && text) progressText.textContent = text;
    };

    const showResult = (downloadURL) => {
      if (!resultSlot || !downloadURL) return;
      resultSlot.innerHTML = `
        <div class="rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-4 flex flex-col md:flex-row gap-4 md:items-center md:justify-between">
          <div>
            <p class="font-semibold text-emerald-300">Arquivo pronto para download</p>
            <p class="text-sm text-slate-300 mt-1">O download será iniciado automaticamente.</p>
          </div>
          <a class="btn-success" href="${downloadURL}">Baixar áudio</a>
        </div>
      `;
      const link = document.createElement("a");
      link.href = downloadURL;
      link.style.display = "none";
      document.body.appendChild(link);
      link.click();
      link.remove();
      showToast("Extração concluída", "success");
    };

    fetch(`/extract/${jobID}`, { method: "GET" }).catch(() => {
      showToast("Não foi possível iniciar a extração", "error");
    });

    const protocol = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${protocol}://${location.host}/ws/${jobID}`);

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        updateProgress(data.progress, data.message || data.status || "Processando...");

        if (data.status === "failed") {
          showToast(data.error || "Falha na extração", "error");
          if (progressText) progressText.textContent = data.error || "Falha na extração";
          ws.close();
          return;
        }

        if (data.status === "completed") {
          updateProgress(100, "Concluído");
          showResult(data.download_url);
          ws.close();
        }
      } catch {
        showToast("Mensagem de progresso inválida", "error");
      }
    };

    ws.onerror = () => {
      showToast("Conexão de progresso perdida", "error");
    };
  };

  setupDropzone();
  setupJobPage();
})();
