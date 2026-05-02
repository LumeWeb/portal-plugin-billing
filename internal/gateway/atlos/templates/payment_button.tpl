{{- define "paymentButtonScript" -}}
(function() {
  // Patch customElements.define to prevent NotSupportedError when ATLOS widget
  // tries to register elements that may already be defined from a previous load.
  if (typeof customElements !== 'undefined' && !customElements.__patched) {
    const originalDefine = customElements.define;
    customElements.define = function(name, constructor, options) {
      if (!customElements.get(name)) {
        originalDefine.call(this, name, constructor, options);
      }
      // Silently skip if already defined
    };
    customElements.__patched = true;
  }

  var buttonId = {{.ButtonID | quote}};

  // Raw payment configuration data from backend (JSON serialized)
  var rawConfigData = {{.ConfigJSON}};

  function dispatchPaymentEvent(eventName, detail) {
    var event = new CustomEvent(eventName, {
      detail: detail,
      bubbles: true
    });
    window.dispatchEvent(event);
    console.log('Payment event dispatched:', eventName, detail);
  }

  // Register cleanup function in global array
  var cleanup = function() {
    var scripts = document.querySelectorAll('script[src*="atlos.io/packages"]');
    scripts.forEach(function(s) { s.remove(); });
    delete globalThis.atlos;
    document.querySelectorAll('atlos-modal').forEach(function(el) { el.remove(); });
    document.querySelectorAll('w3m-modal').forEach(function(el) { el.remove(); });
  };

  (globalThis.__PAYMENT_CLEANUP = globalThis.__PAYMENT_CLEANUP || []).push(cleanup);

  function initButton() {
    if (typeof window.atlos === 'undefined') {
      setTimeout(initButton, 100);
      return;
    }

    var button = document.getElementById(buttonId);
    if (!button) {
      console.error('Payment button not found:', buttonId);
      return;
    }

    button.addEventListener('click', function() {
      try {
        // Merge raw config data with UI event handlers
        var paymentConfig = {
          ...rawConfigData,
          onSuccess: function(response) {
            console.log('Payment successful:', response);
            dispatchPaymentEvent('paymentSuccess', null);
          },
          onCanceled: function(response) {
            console.log('Payment canceled:', response);
            dispatchPaymentEvent('paymentCanceled', null);
          },
          onCompleted: function(response) {
            console.log('Payment completed:', response);
            dispatchPaymentEvent('paymentCompleted', null);
          },
          onError: function(error) {
            console.error('Payment error:', error);
            dispatchPaymentEvent('paymentError', { error: error.message || error });
          }
        };

        window.atlos.Pay(paymentConfig);
      } catch (error) {
        console.error('Failed to initialize ATLOS payment:', error);
        dispatchPaymentEvent('paymentError', { error: error.message || error });
      }
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initButton);
  } else {
    initButton();
  }
})();
{{- end -}}
