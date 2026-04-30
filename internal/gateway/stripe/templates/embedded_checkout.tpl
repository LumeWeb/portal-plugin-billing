<div id="stripe-checkout-container">
	<div id="stripe-checkout"></div>
</div>
<script src="https://js.stripe.com/dahlia/stripe.js"></script>
<script>
(function() {
	var stripe = globalThis.Stripe('{{.PublishableKey}}');
	var checkoutInstance = null;

	async function initialize() {
		checkoutInstance = await stripe.createEmbeddedCheckoutPage({
			fetchClientSecret: async () => '{{.ClientSecret}}'
		});

		checkoutInstance.mount('#stripe-checkout');
	}

	window.dispatchEvent(new CustomEvent('paymentCleanupRegister', {
		detail: {
			cleanup: function() {
				if (checkoutInstance) {
					checkoutInstance.unmount();
				}
				var container = document.getElementById('stripe-checkout');
				if (container) { container.innerHTML = ''; }
				var scripts = document.querySelectorAll('script[src*="js.stripe.com"]');
				scripts.forEach(function(s) { s.remove(); });
				delete globalThis.Stripe;
			}
		},
		bubbles: true
	}));

	initialize();
})();
</script>
